package config

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
)

type FileDownloader interface {
	Get(url string) (resp *http.Response, err error)
}

type FileDownloaderImpl struct {
	FileDownloader
}

func (fd FileDownloaderImpl) Get(url string) (resp *http.Response, err error) {
	return http.Get(url)
}

// CheckPluginExists this function checks if the plugin exists in the configured path if not we panic
func checkPluginExists(pluginLocation string) error {
	if pluginLocation != "" {
		//Check for plugin executable existence
		_, err := os.Stat(pluginLocation)
		if err != nil {
			return err
		}
	}
	return nil
}

func checkShaOfPlugin(pluginLocation string, expectedSha256 string) (bool, error) {
	hasher := sha256.New()
	fileBytes, err := os.ReadFile(pluginLocation)
	if err != nil {
		return false, err
	}
	fileSha256 := fmt.Sprintf("%x", hasher.Sum(fileBytes))
	match := fileSha256 == expectedSha256
	if !match {
		log.Printf("expected sha256: %s, actual sha256: %s, of downloaded metric plugin (%s)", expectedSha256, fileSha256, pluginLocation)
	}
	return match, nil
}

func downloadFile(filepath string, url string, downloader FileDownloader) error {
	// Get the data
	resp, err := downloader.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// Set the file permissions, to allow execution
	err = os.Chmod(filepath, 0700)
	if err != nil {
		return err
	}

	return err
}

// initMetricsPlugins this function downloads and/or checks that a plugin executable exits on the filesystem
func initMetricsPlugins(fd FileDownloader) error {
	config, err := GetConfig()
	if err != nil {
		return err
	}
	for _, plugin := range config.GetMetricPluginsConfig() {
		urlObj, err := url.ParseRequestURI(plugin.PluginLocation)
		finalFileLocation := filepath.Join("/tmp", plugin.Name)
		if err != nil {
			return err
		}

		switch urlObj.Scheme {
		case "http", "https":
			log.Printf("Downloading plugin from: %s", plugin.PluginLocation)
			startTime := time.Now()
			err = downloadFile(finalFileLocation, urlObj.String(), fd)
			if err != nil {
				return err
			}
			timeTakenToDownload := time.Now().Sub(startTime)
			log.Printf("Download complete, it took %s", timeTakenToDownload)

			if plugin.PluginSha256 != "" {
				sha256Matched, err := checkShaOfPlugin(finalFileLocation, plugin.PluginSha256)
				if err != nil {
					return err
				}
				if !sha256Matched {
					return fmt.Errorf("sha256 hash of downloaded plugin (%s) does not match expected hash", plugin.PluginLocation)
				}
			}
			if checkPluginExists(finalFileLocation) != nil {
				return fmt.Errorf("failed to find plugin at location: %s", plugin.PluginLocation)
			}

		case "file":
			pluginPath, err := filepath.Abs(urlObj.Host + urlObj.Path)
			if err != nil {
				return err
			}

			if err = copyFile(pluginPath, finalFileLocation); err != nil {
				return err
			}
			if checkPluginExists(finalFileLocation) != nil {
				return fmt.Errorf("failed to find plugin at location: %s", plugin.PluginLocation)
			}
		default:
			return fmt.Errorf("plugin location must be of http(s) or file scheme")
		}
	}

	return nil
}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
func copyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	if err = os.Link(src, dst); err == nil {
		return
	}
	return
}
