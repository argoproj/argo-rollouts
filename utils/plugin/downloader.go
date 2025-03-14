package plugin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	argoConfig "github.com/argoproj/argo-rollouts/utils/config"

	"github.com/argoproj/argo-rollouts/utils/defaults"

	log "github.com/sirupsen/logrus"
)

// FileDownloader is an interface that allows us to mock the http.Get function
type FileDownloader interface {
	Get(url string, header http.Header) (resp *http.Response, err error)
}

// FileDownloaderImpl is the default/real implementation of the FileDownloader interface
type FileDownloaderImpl struct {
}

func (fd FileDownloaderImpl) Get(url string, header http.Header) (resp *http.Response, err error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header = header
	return http.DefaultClient.Do(request)
}

// checkPluginExists this function checks if the plugin exists in the configured path on the filesystem
func checkPluginExists(pluginLocation string) error {
	if pluginLocation != "" {
		//Check for plugin executable existence
		_, err := os.Stat(pluginLocation)
		if err != nil {
			return fmt.Errorf("plugin stat file at %s", pluginLocation)
		}
	}
	return nil
}

func checkShaOfPlugin(pluginLocation string, expectedSha256 string) (bool, error) {
	fileBytes, err := os.ReadFile(pluginLocation)
	if err != nil {
		return false, fmt.Errorf("failed to read file %s: %w", pluginLocation, err)
	}
	var fileSha256 string
	if len(expectedSha256) == 64 {
		fileSha256 = fmt.Sprintf("%x", sha256.Sum256(fileBytes))
	} else {
		hasher := sha256.New()
		fileSha256 = fmt.Sprintf("%x", hasher.Sum(fileBytes))
	}
	match := fileSha256 == expectedSha256
	if !match {
		log.Printf("expected sha256: %s, actual sha256: %s, of downloaded metric plugin (%s)", expectedSha256, fileSha256, pluginLocation)
	}
	return match, nil
}

func downloadFile(filepath string, url string, downloader FileDownloader, header http.Header) error {
	// Get the data with credentials
	resp, err := downloader.Get(url, header)
	if err != nil {
		return fmt.Errorf("failed to download file from %s: %w", url, err)
	}

	if isFailure(resp.StatusCode) {
		return fmt.Errorf("failed to download file from %s: response code %s", url, http.StatusText(resp.StatusCode))
	}

	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filepath, err)
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %w", filepath, err)
	}

	// Set the file permissions, to allow execution
	err = os.Chmod(filepath, 0700)
	if err != nil {
		return fmt.Errorf("failed to set file permissions on %s: %w", filepath, err)
	}

	return err
}

// DownloadPlugins this function downloads and/or checks that a plugin executable exits on the filesystem
func DownloadPlugins(fd FileDownloader, kubeClient kubernetes.Interface) error {
	config, err := argoConfig.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	absoluteFilepath, err := filepath.Abs(defaults.DefaultRolloutPluginFolder)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of plugin folder: %w", err)
	}

	for _, plugin := range config.GetAllPlugins() {
		urlObj, err := url.ParseRequestURI(plugin.Location)
		if err != nil {
			return fmt.Errorf("failed to parse plugin location: %w", err)
		}

		dir, pluginFile, err := argoConfig.GetPluginDirectoryAndFilename(plugin.Name)
		if err != nil {
			return fmt.Errorf("failed to convert plugin name (%s) to directory and filename: (%w)", plugin.Name, err)
		}

		finalFolderLocation := filepath.Join(absoluteFilepath, dir)
		err = os.MkdirAll(finalFolderLocation, 0700)
		if err != nil {
			return fmt.Errorf("failed to create plugin folder for plugin (%s): (%w)", plugin.Name, err)
		}

		finalFileLocation := filepath.Join(finalFolderLocation, pluginFile)

		switch urlObj.Scheme {
		case "http", "https":
			log.Infof("Downloading plugin %s from: %s", plugin.Name, plugin.Location)
			startTime := time.Now()
			requestHeader := http.Header{}
			for _, header := range plugin.HeadersFrom {
				secret, err := kubeClient.CoreV1().Secrets(defaults.Namespace()).Get(context.Background(), header.SecretRef.Name, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to get secret in secretRef: %w", err)
				}
				for k, v := range secret.Data {
					requestHeader.Add(k, string(v))
				}
			}

			err = downloadFile(finalFileLocation, urlObj.String(), fd, requestHeader)
			if err != nil {
				return fmt.Errorf("failed to download plugin from %s: %w", plugin.Location, err)
			}
			timeTakenToDownload := time.Now().Sub(startTime)
			log.Infof("Download complete, it took %s", timeTakenToDownload)

			if plugin.Sha256 != "" {
				sha256Matched, err := checkShaOfPlugin(finalFileLocation, plugin.Sha256)
				if err != nil {
					return fmt.Errorf("failed to check sha256 of downloaded plugin: %w", err)
				}
				if !sha256Matched {
					return fmt.Errorf("sha256 hash of downloaded plugin (%s) does not match expected hash", plugin.Location)
				}
			}
			if checkPluginExists(finalFileLocation) != nil {
				return fmt.Errorf("failed to find downloaded plugin at location: %s", plugin.Location)
			}

		case "file":
			pluginPath, err := filepath.Abs(urlObj.Host + urlObj.Path)
			if err != nil {
				return fmt.Errorf("failed to get absolute path of plugin: %w", err)
			}

			if err := copyFile(pluginPath, finalFileLocation); err != nil {
				return fmt.Errorf("failed to copy plugin from %s to %s: %w", pluginPath, finalFileLocation, err)
			}

			log.Infof("Copied plugin from %s to %s", pluginPath, finalFileLocation)
			if checkPluginExists(finalFileLocation) != nil {
				return fmt.Errorf("failed to find filebased plugin at location: %s", plugin.Location)
			}
			// Set the file permissions, to allow execution
			err = os.Chmod(finalFileLocation, 0700)
			if err != nil {
				return fmt.Errorf("failed to set file permissions of plugin (%s): %w", finalFileLocation, err)
			}
		default:
			return fmt.Errorf("plugin location must be of http(s) or file scheme")
		}
	}

	return nil
}

// CopyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to get file stat of %s: %w", src, err)
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	if err != nil {
		return fmt.Errorf("failed to copy file from %s to %s: %w", src, dst, err)
	}
	return nil
}

// isFailure determines if the response has a 2xx response
func isFailure(statusCode int) bool {
	return statusCode < http.StatusOK || statusCode >= http.StatusBadRequest
}
