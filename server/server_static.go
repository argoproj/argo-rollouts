package server

import (
	"embed"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

var (
	//go:embed static/*
	static         embed.FS //nolint
	staticBasePath = "static"
	indexHtmlFile  = staticBasePath + "/index.html"
)

const (
	ContentType   = "Content-Type"
	ContentLength = "Content-Length"
)

func (s *ArgoRolloutsServer) staticFileHttpHandler(w http.ResponseWriter, r *http.Request) {
	requestedURI := path.Clean(r.RequestURI)
	rootPath := path.Clean("/" + s.Options.RootPath)

	if requestedURI == "/" {
		http.Redirect(w, r, rootPath+"/", http.StatusFound)
		return
	}

	//If the rootPath is not in the prefix 404
	if !strings.HasPrefix(requestedURI, rootPath) {
		http.NotFound(w, r)
		return
	}

	embedPath := path.Join(staticBasePath, strings.TrimPrefix(requestedURI, rootPath))

	//If the rootPath is the requestedURI, serve index.html
	if requestedURI == rootPath {
		embedPath = indexHtmlFile
	}

	fileBytes, err := static.ReadFile(embedPath)
	if err != nil {
		if fileNotExistsOrIsDirectoryError(err) {
			// send index.html, because UI will use path based router in React
			fileBytes, _ = static.ReadFile(indexHtmlFile)
			embedPath = indexHtmlFile
		} else {
			log.Errorf("Error reading file %s: %v", embedPath, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	if embedPath == indexHtmlFile {
		fileBytes = withRootPath(fileBytes, s.Options.RootPath)
	}

	w.Header().Set(ContentType, determineMimeType(embedPath))
	w.Header().Set(ContentLength, strconv.Itoa(len(fileBytes)))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(fileBytes)
	if err != nil {
		log.Errorf("Error writing response: %v", err)
	}
}

func fileNotExistsOrIsDirectoryError(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	pathErr, isPathError := err.(*fs.PathError)
	return isPathError && strings.Contains(pathErr.Error(), "is a directory")
}

func determineMimeType(fileName string) string {
	idx := strings.LastIndex(fileName, ".")
	if idx >= 0 {
		mimeType := mime.TypeByExtension(fileName[idx:])
		if len(mimeType) > 0 {
			return mimeType
		}
	}
	return "text/plain"
}

var re = regexp.MustCompile(`<base href=".*".*/>`)

func withRootPath(fileContent []byte, rootpath string) []byte {
	var temp = re.ReplaceAllString(string(fileContent), `<base href="`+path.Clean("/"+rootpath)+`/" />`)
	return []byte(temp)
}
