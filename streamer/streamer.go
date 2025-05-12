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
	"time"
)

// Global buffer pool to hold 4MB buffers
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 4*1024*1024) // 4MB 缓冲区
	},
}

func init() {
	// Pre-warm the buffer pool with 200 buffers
	for i := 0; i < 200; i++ {
		bufferPool.Put(make([]byte, 4*1024*1024))
	}
}

func Stream(c *gin.Context, filePath string) {
	startTime := time.Now()
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
	logger.Debug("File size retrieved", "filePath", filePath, "fileSize", fileSize, "elapsed", time.Since(startTime))

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
	startTime := time.Now()
	file, err := os.Open(filePath)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return nil, err
	}
	logger.Debug("File opened successfully", "filePath", filePath, "elapsed", time.Since(startTime))
	return file, nil
}

func getFileInfo(c *gin.Context, file *os.File) (os.FileInfo, error) {
	startTime := time.Now()
	fileInfo, err := file.Stat()
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return nil, err
	}
	logger.Debug("File info retrieved", "fileName", fileInfo.Name(), "fileSize", fileInfo.Size(), "elapsed", time.Since(startTime))
	return fileInfo, nil
}

func parseRangeHeader(c *gin.Context, fileSize int64) (int64, int64) {
	startTime := time.Now()
	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		logger.Debug("No Range header provided, returning full file", "elapsed", time.Since(startTime))
		return 0, fileSize - 1
	}

	logger.Debug("Original Range header received", "rangeHeader", rangeHeader)
	ranges := strings.SplitN(rangeHeader, "=", 2)
	if len(ranges) != 2 || ranges[0] != "bytes" {
		logger.Warn("Invalid range header format, falling back to full file", "rangeHeader", rangeHeader, "elapsed", time.Since(startTime))
		return 0, fileSize - 1
	}

	rangeParts := strings.SplitN(ranges[1], "-", 2)
	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil || start < 0 || start >= fileSize {
		logger.Warn("Invalid start range, falling back to full file", "start", rangeParts[0], "fileSize", fileSize, "elapsed", time.Since(startTime))
		return 0, fileSize - 1
	}

	var end int64
	if rangeParts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(rangeParts[1], 10, 64)
		if err != nil || end >= fileSize || end < start {
			logger.Warn("Invalid end range, adjusting to file end", "end", rangeParts[1], "fileSize", fileSize, "elapsed", time.Since(startTime))
			end = fileSize - 1
		}
	}

	logger.Debug("Range header parsed", "start", start, "end", end, "elapsed", time.Since(startTime))
	return start, end
}

func streamFullFile(c *gin.Context, file *os.File, fileInfo os.FileInfo, start, end int64) {
	fileSize := fileInfo.Size()
	contentType := getFileContentType(fileInfo)

	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
	c.Writer.Header().Set("Accept-Ranges", "bytes")

	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" {
		c.Status(http.StatusPartialContent)
		c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
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
	streamFile(file, c, start, end)
}

func streamPartialFile(c *gin.Context, file *os.File, fileInfo os.FileInfo, start, end int64) {
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
	startTime := time.Now()
	// Use smaller buffer for initial chunk to speed up first response
	bufferSize := 1 * 1024 // 256KB for initial chunk
	if start != 0 {
		bufferSize = 4 * 1024 * 1024 // 4MB for subsequent chunks
	}

	// Fetch or adjust buffer from pool
	bufferGetTime := time.Now()
	buffer := bufferPool.Get().([]byte)
	if len(buffer) != bufferSize {
		buffer = make([]byte, bufferSize) // Dynamically adjust if needed
	}
	logger.Debug("Buffer acquired", "size", bufferSize, "elapsed", time.Since(bufferGetTime))
	defer bufferPool.Put(buffer)

	// Seek to the start position
	seekStartTime := time.Now()
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		logger.Error("Error seeking file", "start", start, "error", err, "elapsed", time.Since(seekStartTime))
		return
	}
	logger.Debug("Seek completed", "start", start, "elapsed", time.Since(seekStartTime))

	totalBytes := end - start + 1
	writtenBytes := int64(0)
	chunkCount := 0

	for totalBytes > 0 {
		chunkStartTime := time.Now()
		chunkCount++
		readSize := int64(len(buffer))
		if totalBytes < readSize {
			readSize = totalBytes
		}

		// Read from file
		readStartTime := time.Now()
		n, err := file.Read(buffer[:readSize])
		if err != nil {
			if err == io.EOF {
				logger.Debug("Read EOF", "chunk", chunkCount, "elapsed", time.Since(readStartTime))
				break
			}
			c.AbortWithStatus(http.StatusInternalServerError)
			logger.Error("Error reading file", "error", err, "chunk", chunkCount, "elapsed", time.Since(readStartTime))
			return
		}
		if n == 0 {
			logger.Debug("Read zero bytes", "chunk", chunkCount, "elapsed", time.Since(readStartTime))
			break
		}
		logger.Debug("Read completed", "chunk", chunkCount, "bytes", n, "elapsed", time.Since(readStartTime))

		// Write to client
		writeStartTime := time.Now()
		_, writeErr := c.Writer.Write(buffer[:n])
		if writeErr != nil {
			logger.Error("Client connection lost", "error", writeErr, "chunk", chunkCount, "elapsed", time.Since(writeStartTime))
			return
		}
		logger.Debug("Write completed", "chunk", chunkCount, "bytes", n, "elapsed", time.Since(writeStartTime))

		writtenBytes += int64(n)
		totalBytes -= int64(n)

		// Flush strategically
		flushStartTime := time.Now()
		if writtenBytes%int64(1024*1024) == 0 || totalBytes == 0 {
			c.Writer.Flush()
			logger.Debug("Flush executed", "chunk", chunkCount, "writtenBytes", writtenBytes, "elapsed", time.Since(flushStartTime))
		}

		logger.Debug("Chunk processed", "chunk", chunkCount, "bytes", n, "remaining", totalBytes, "elapsed", time.Since(chunkStartTime))
	}

	// Final flush
	finalFlushStartTime := time.Now()
	c.Writer.Flush()
	logger.Debug("Final flush executed", "elapsed", time.Since(finalFlushStartTime))

	logger.Info("File streaming completed", "start", start, "end", end, "totalBytes", end-start+1, "totalElapsed", time.Since(startTime))
}

func getFileContentType(fileInfo os.FileInfo) string {
	name := strings.ToLower(fileInfo.Name())
	switch {
	case strings.HasSuffix(name, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(name, ".mkv"):
		return "video/x-matroska"
	case strings.HasSuffix(name, ".avi"):
		return "video/x-msvideo"
	case strings.HasSuffix(name, ".mov"):
		return "video/quicktime"
	case strings.HasSuffix(name, ".flv"):
		return "video/x-flv"
	case strings.HasSuffix(name, ".rmvb"):
		return "application/vnd.rn-realmedia-vbr"
	case strings.HasSuffix(name, ".rm"):
		return "application/vnd.rn-realmedia"
	case strings.HasSuffix(name, ".mka"):
		return "audio/x-matroska"
	case strings.HasSuffix(name, ".aac"):
		return "audio/aac"
	case strings.HasSuffix(name, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(name, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(name, ".ogg"):
		return "audio/ogg"
	case strings.HasSuffix(name, ".srt"):
		return "application/x-subrip"
	case strings.HasSuffix(name, ".vtt"):
		return "text/vtt"
	case strings.HasSuffix(name, ".ass"):
		return "text/x-ssa"
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".gif"):
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}
