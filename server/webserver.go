package server

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"text/template"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type WebserverHandler struct {
	fallbackFilepath string
	fileSystem       fs.FS
	config           *Config
	templateServer   *FileReplaceHandler
}

func New(fileSystem fs.FS, fallbackFilepath string, config *Config) *WebserverHandler {
	handler := &WebserverHandler{
		fallbackFilepath: fallbackFilepath,
		fileSystem:       fileSystem,
		config:           config,
		templateServer: &FileReplaceHandler{
			Filesystem: fileSystem,
			Templates:  make(map[string]*template.Template),
		},
	}
	return handler
}

func (handler *WebserverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	logger := log.Ctx(r.Context())
	logger.Debug().Msg("Entering webserver handler")
	requestPath := r.URL.Path
	logger.Debug().Msgf("Serving file %s", requestPath)

	file, err := handler.tryGetFile(requestPath)
	if err != nil {
		logger.Debug().Err(err).Msgf("file %s not found", requestPath)
		var finishServing bool
		file, requestPath, finishServing = handler.checkForFallbackFile(logger, w, requestPath)
		if finishServing {
			return
		}
	}
	defer file.Close()
	w.Header().Set("Content-Type", handler.getMediaType(requestPath))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, err = io.Copy(w, file)
	if err != nil {
		log.Warn().Err(err).Msg("error copying requested file")
		http.Error(w, "failed to copy requested file, you can retry.", http.StatusInternalServerError)
		return
	}
}

func (handler *WebserverHandler) tryGetFile(requestPath string) (fs.File, error) {
	file, err := handler.fileSystem.Open(requestPath)
	if err != nil {
		return nil, err
	}
	fileInfo, err := file.Stat()
	if fileInfo.IsDir() {
		defer file.Close()
		return nil, fmt.Errorf("requested file is directory")
	}
	return file, err
}

func (handler *WebserverHandler) checkForFallbackFile(logger *zerolog.Logger, w http.ResponseWriter, requestPath string) (file fs.File, requestpath string, finishServing bool) {
	// requested files do not fall back to index.html
	if handler.fallbackFilepath == "" || (path.Ext(requestPath) != "" && path.Ext(requestPath) != ".") {
		http.Error(w, "file not found", http.StatusNotFound)
		return nil, "", true
	}
	requestPath = handler.fallbackFilepath
	file, err := handler.fileSystem.Open(handler.fallbackFilepath)
	if err != nil {
		logger.Error().Err(err).Msg("fallback file not found")
		http.Error(w, "file not found", http.StatusNotFound)
		return nil, "", true
	}
	return file, requestPath, false
}

func (handler *WebserverHandler) getMediaType(requestPath string) string {
	mediaType, ok := handler.config.MediaTypeMap[path.Ext(requestPath)]
	if !ok {
		mediaType = "application/octet-stream"
	}
	return mediaType
}