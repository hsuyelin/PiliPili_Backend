package streamer

import (
	"PiliPili_Backend/logger"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Global buffer pool to hold 4MB buffers
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 4*1024*1024) // 4MB 缓冲区
	},
}

func Stream(c *gin.Context, filePath string) {
	logger.Info("Starting file streaming", "filePath", filePath)

	file, err := getFile(c, filePath)
	if err != nil {
		logger.Error("Failed to open file", "filePath", filePath, "error", err)
		return
	}

	defer func() {
		if err := file.Close(); err != nil {
			logger.Error("Error closing file", "filePath", filePath, "error", err)
		}
	}()

	fileInfo, err := getFileInfo(c, file)
	if err != nil {
		logger.Error("Failed to get file info", "filePath", filePath, "error", err)
		return
	}

	fileSize := fileInfo.Size()
	logger.Debug("File size retrieved", "filePath", filePath, "fileSize", fileSize)

	start, end := parseRangeHeader(c, fileSize)

	if start == 0 && end == fileSize-1 {
		logger.Info("Full file request", "filePath", filePath)
		streamFullFile(c, file, fileInfo, start, end)
	} else {
		logger.Info("Partial file request", "filePath", filePath, "start", start, "end", end)
		streamPartialFile(c, file, fileInfo, start, end)
	}
}

func getFile(c *gin.Context, filePath string) (*os.File, error) {
	file, err := os.Open(filePath)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return nil, err
	}
	logger.Debug("File opened successfully", "filePath", filePath)
	return file, nil
}

func getFileInfo(c *gin.Context, file *os.File) (os.FileInfo, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return nil, err
	}
	logger.Debug("File info retrieved", "fileName", fileInfo.Name(), "fileSize", fileInfo.Size())
	return fileInfo, nil
}

func parseRangeHeader(c *gin.Context, fileSize int64) (int64, int64) {
	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		logger.Debug("No Range header provided, returning full file")
		return 0, fileSize - 1
	}

	logger.Debug("Original Range header received", "rangeHeader", rangeHeader)
	ranges := strings.Split(rangeHeader, "=")
	if len(ranges) != 2 || ranges[0] != "bytes" {
		logger.Warn("Invalid range header format, falling back to full file", "rangeHeader", rangeHeader)
		return 0, fileSize - 1
	}

	rangeParts := strings.Split(ranges[1], "-")
	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil || start < 0 || start >= fileSize {
		logger.Warn("Invalid start range, falling back to full file", "start", start, "fileSize", fileSize)
		return 0, fileSize - 1
	}

	var end int64
	if rangeParts[1] != "" {
		end, err = strconv.ParseInt(rangeParts[1], 10, 64)
		if err != nil || end >= fileSize || end < start {
			logger.Warn("Invalid end range, falling back to full file", "end", end, "fileSize", fileSize)
			return 0, fileSize - 1
		}
	} else {
		end = fileSize - 1
	}

	logger.Debug("Range header parsed", "start", start, "end", end)
	return start, end
}

func streamFullFile(
	c *gin.Context,
	file *os.File,
	fileInfo os.FileInfo,
	start, end int64,
) {
	fileSize := fileInfo.Size()
	contentType := getFileContentType(fileInfo)

	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
	c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Writer.Header().Set("Accept-Ranges", "bytes")

	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" {
		c.Status(http.StatusPartialContent)
		contentRange := fmt.Sprintf("bytes 0-%d/%d", fileSize-1, fileSize)
		c.Writer.Header().Set("Content-Range", contentRange)
	} else {
		c.Status(http.StatusOK)
	}

	logger.Info(
		"Streaming full file",
		"fileName", fileInfo.Name(),
		"requestHeaders", c.Request.Header,
		"responseHeaders", c.Writer.Header(),
		"fileSize", fileSize,
	)
	streamFile(file, c, 0, fileSize-1)
}

func streamPartialFile(
	c *gin.Context,
	file *os.File,
	fileInfo os.FileInfo,
	start, end int64,
) {
	fileSize := fileInfo.Size()
	contentType := getFileContentType(fileInfo)
	contentLength := end - start + 1

	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Writer.Header().Set("Accept-Ranges", "bytes")
	c.Status(http.StatusPartialContent)

	logger.Info(
		"Streaming partial file",
		"fileName", fileInfo.Name(),
		"start", start,
		"end", end,
		"contentLength", contentLength,
		"fileSize", fileSize,
		"requestHeaders", c.Request.Header,
		"responseHeaders", c.Writer.Header(),
	)
	streamFile(file, c, start, end)
}

func streamFile(file *os.File, c *gin.Context, start, end int64) {
	buffer := bufferPool.Get().([]byte)
	defer bufferPool.Put(buffer)

	_, err := file.Seek(start, 0)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		logger.Error("Error seeking file", "start", start, "error", err)
		return
	}

	totalBytes := end - start + 1
	for totalBytes > 0 {
		readSize := int64(len(buffer))
		if totalBytes < readSize {
			readSize = totalBytes
		}

		n, err := file.Read(buffer[:readSize])
		if err != nil {
			if err == io.EOF {
				break
			}
			c.AbortWithStatus(http.StatusInternalServerError)
			logger.Error("Error reading file", "error", err)
			return
		}

		if n == 0 {
			break
		}

		_, writeErr := c.Writer.Write(buffer[:n])
		if writeErr != nil {
			logger.Error("Client connection lost", "error", writeErr)
			return
		}

		c.Writer.Flush()
		totalBytes -= int64(n)
	}

	logger.Info("File streaming completed")
}

func getFileContentType(fileInfo os.FileInfo) string {
	name := strings.ToLower(fileInfo.Name())
	if strings.HasSuffix(name, ".mp4") {
		return "video/mp4"
	}
	if strings.HasSuffix(name, ".mkv") {
		return "video/x-matroska"
	}
	if strings.HasSuffix(name, ".avi") {
		return "video/x-msvideo"
	}
	if strings.HasSuffix(name, ".mov") {
		return "video/quicktime"
	}
	if strings.HasSuffix(name, ".flv") {
		return "video/x-flv"
	}
	if strings.HasSuffix(name, ".rmvb") {
		return "application/vnd.rn-realmedia-vbr"
	}
	if strings.HasSuffix(name, ".rm") {
		return "application/vnd.rn-realmedia"
	}
	if strings.HasSuffix(name, ".mka") {
		return "audio/x-matroska"
	}
	if strings.HasSuffix(name, ".aac") {
		return "audio/aac"
	}
	if strings.HasSuffix(name, ".mp3") {
		return "audio/mpeg"
	}
	if strings.HasSuffix(name, ".wav") {
		return "audio/wav"
	}
	if strings.HasSuffix(name, ".ogg") {
		return "audio/ogg"
	}
	if strings.HasSuffix(name, ".srt") {
		return "application/x-subrip"
	}
	if strings.HasSuffix(name, ".vtt") {
		return "text/vtt"
	}
	if strings.HasSuffix(name, ".ass") {
		return "text/x-ssa"
	}
	if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") {
		return "image/jpeg"
	}
	if strings.HasSuffix(name, ".png") {
		return "image/png"
	}
	if strings.HasSuffix(name, ".gif") {
		return "image/gif"
	}
	return "application/octet-stream"
}
