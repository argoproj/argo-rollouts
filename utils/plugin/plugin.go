package plugin

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/time"
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
func checkPluginExists() error {
	if defaults.GetMetricPluginLocation() != "" {
		//Check for plugin executable existence
		_, err := os.Stat(defaults.GetMetricPluginLocation())
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
	log.Printf("exected sha256: %s, actual sha256: %s, of downloaded metric plugin", expectedSha256, fileSha256)
	return fileSha256 == expectedSha256, nil
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

// InitMetricsPlugin this function downloads and/or checks that a plugin executable exits on the filesystem
func InitMetricsPlugin(pluginPath string, fd FileDownloader, expectedSha256Hash string) error {
	if pluginPath == "" {
		return nil
	}

	urlObj, err := url.ParseRequestURI(pluginPath)
	if err != nil {
		return err
	}

	switch urlObj.Scheme {
	case "http", "https":
		log.Printf("Downloading plugin from: %s", pluginPath)
		startTime := time.Now()
		err = downloadFile(defaults.DefaultPluginHttpFileLocation, urlObj.String(), fd)
		if err != nil {
			return err
		}
		timeTakenToDownload := time.Now().Sub(startTime)
		log.Printf("Download complete, it took %s", timeTakenToDownload)
		defaults.SetMetricPluginLocation("file://" + defaults.DefaultPluginHttpFileLocation)

		if expectedSha256Hash != "" {
			sha256Matched, err := checkShaOfPlugin(defaults.DefaultPluginHttpFileLocation, expectedSha256Hash)
			if err != nil {
				return err
			}
			if !sha256Matched {
				return fmt.Errorf("sha256 hash of downloaded plugin does not match expected hash")
			}
		}
	case "file":
		pluginPath, err = filepath.Abs(urlObj.Host + urlObj.Path)
		if err != nil {
			return err
		}
		defaults.SetMetricPluginLocation(urlObj.Scheme + "://" + pluginPath)
	default:
		return fmt.Errorf("plugin location must be of http(s) or file scheme")
	}

	return checkPluginExists()
}
