package streamer

import (
	"PiliPili_Backend/logger"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Global buffer pool to hold 4MB buffers
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 4*1024*1024) // 4MB buffer
	},
}

// File handle cache with reference counting
type fileEntry struct {
	file     *os.File
	refCount int       // Number of active requests using this file handle
	lastUsed time.Time // Last time this file handle was accessed
}

var fileCache = struct {
	sync.Mutex
	cache map[string]*fileEntry
}{
	cache: make(map[string]*fileEntry),
}

// fileMu guards access to individual file handles (Seek, Read, Stat, Close)
// We use sync.Map to store mutexes for each file path, allowing finer-grained locking.
var fileMu sync.Map

// Content cache for initial file chunks
var contentCache = struct {
	sync.Mutex
	cache map[string][]byte
}{
	cache: make(map[string][]byte),
}

// Configurable: Maximum number of cached file handles
const maxCachedFiles = 200

// Interval for cleaning up unused file handles in the cache
const fileCacheCleanupInterval = 5 * time.Minute

// Time-to-live for unused file handles before cleanup
const fileCacheEntryTTL = 10 * time.Minute

// Interval for cleaning up unused content cache entries
const contentCacheCleanupInterval = 10 * time.Minute

// Configurable: Size of content cache for initial file chunks
const initialContentCacheSize = 2 * 1024 * 1024

// Flush frequency
const flushChunkSize = 256 * 1024

func init() {
	// Pre-warm the buffer pool with 200 buffers
	for i := 0; i < 200; i++ {
		bufferPool.Put(make([]byte, 4*1024*1024))
	}

	// Periodically clean up unused file handles
	go func() {
		for {
			time.Sleep(fileCacheCleanupInterval)
			fileCache.Lock()
			var pathsToCleanup []string
			for path, entry := range fileCache.cache {
				if entry.refCount == 0 && time.Since(entry.lastUsed) > fileCacheEntryTTL {
					pathsToCleanup = append(pathsToCleanup, path)
				}
			}
			fileCache.Unlock()

			for _, path := range pathsToCleanup {
				mu, loaded := fileMu.Load(path)
				if !loaded {
					continue
				}
				mutex := mu.(*sync.Mutex)
				mutex.Lock()

				fileCache.Lock()
				if entry, exists := fileCache.cache[path]; exists && entry.refCount == 0 && time.Since(entry.lastUsed) > fileCacheEntryTTL {
					if err := entry.file.Close(); err != nil {
						logger.Error("Error closing cached file during cleanup", "filePath", path, "error", err)
					}
					delete(fileCache.cache, path)
					fileMu.Delete(path)
					logger.Info("Closed and removed cached file and mutex during cleanup", "filePath", path)

					contentCache.Lock()
					delete(contentCache.cache, path)
					contentCache.Unlock()
					logger.Debug("Cleared content cache during file cleanup", "filePath", path)
				}
				fileCache.Unlock()

				mutex.Unlock()
			}
		}
	}()

	// Clean up content cache periodically
	go func() {
		for {
			time.Sleep(contentCacheCleanupInterval)
			contentCache.Lock()
			var pathsToCleanup []string
			for path := range contentCache.cache {
				fileCache.Lock()
				entry, exists := fileCache.cache[path]
				fileCache.Unlock()

				if !exists || entry.refCount == 0 {
					pathsToCleanup = append(pathsToCleanup, path)
				}
			}
			for _, path := range pathsToCleanup {
				delete(contentCache.cache, path)
				logger.Info("Cleared content cache", "filePath", path)
			}
			contentCache.Unlock()
		}
	}()
}

// preloadFileContent opens a file, reads initial content, and adds to content cache.
// This function is called synchronously within openAndCacheFile.
// It assumes the caller holds the per-file mutex for filePath.
func preloadFileContent(filePath string, file *os.File) {
	// The per-file mutex is held by the caller (openAndCacheFile)
	// We still need to lock contentCache.
	contentCache.Lock()
	defer contentCache.Unlock()

	if _, exists := contentCache.cache[filePath]; exists {
		return
	}

	buffer := make([]byte, initialContentCacheSize)
	// Use ReadAt to read from the beginning without changing file's current offset
	// ReadAt is safe to call concurrently on the same file handle IF the underlying
	// OS supports it and IF the offset is not modified by other operations.
	// Since the per-file mutex is held by the caller, concurrent Seek/Read operations
	// on this specific file handle are prevented, making ReadAt safe here.
	n, err := file.ReadAt(buffer, 0)
	if err != nil && err != io.EOF {
		logger.Error("Failed to preload file content", "filePath", filePath, "error", err)
		return
	}
	contentCache.cache[filePath] = buffer[:n]
	logger.Debug("Preloaded file content", "filePath", filePath, "bytes", n)
}

func Stream(c *gin.Context, filePath string) {
	startTime := time.Now()
	logger.Info("Starting file streaming", "filePath", filePath)

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		logger.Error("Failed to get absolute path", "filePath", filePath, "error", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	mu, _ := fileMu.LoadOrStore(absPath, &sync.Mutex{})
	mutex := mu.(*sync.Mutex)
	mutex.Lock()
	defer mutex.Unlock()

	file, isCached, err := getFileFromCacheOrOpen(absPath)
	if err != nil {
		logger.Error("Failed to get file", "filePath", absPath, "error", err)
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if !isCached {
		defer func() {
			if err := file.Close(); err != nil {
				logger.Error("Error closing non-cached file", "filePath", absPath, "error", err)
			} else {
				logger.Debug("Closed non-cached file", "filePath", absPath)
			}
		}()
	} else {
		defer func() {
			fileCache.Lock()
			if entry, exists := fileCache.cache[absPath]; exists && entry.file == file {
				entry.refCount--
				if entry.refCount == 0 {
					entry.lastUsed = time.Now()
				} else if entry.refCount < 0 {
					logger.Warn(
						"Negative ref count, likely a bug",
						"filePath", absPath,
						"refCount", entry.refCount,
					)
					entry.refCount = 0
				}
			}
			fileCache.Unlock()
		}()
	}

	fileInfo, err := file.Stat()
	if err != nil {
		logger.Error("Failed to get file info", "filePath", absPath, "error", err)
		fileCache.Lock()
		if entry, exists := fileCache.cache[absPath]; exists && entry.file == file {
			if closeErr := entry.file.Close(); closeErr != nil {
				logger.Error(
					"Error closing invalid cached file handle during Stat error",
					"filePath", absPath, "error", closeErr,
				)
			}
			delete(fileCache.cache, absPath)
			fileMu.Delete(absPath)
			logger.Warn(
				"Invalid cached file handle, removed from cache",
				"filePath", absPath,
				"error", err,
			)
		}
		fileCache.Unlock()

		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	fileSize := fileInfo.Size()
	logger.Debug(
		"File size retrieved",
		"filePath", absPath,
		"fileSize", fileSize,
		"elapsed", time.Since(startTime),
	)

	start, end := parseRangeHeader(c, fileSize)

	if start >= fileSize {
		logger.Warn(
			"Requested start is beyond file size",
			"filePath", absPath,
			"start", start,
			"fileSize", fileSize,
		)
		c.AbortWithStatus(http.StatusRequestedRangeNotSatisfiable)
		c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		return
	}
	if end >= fileSize {
		end = fileSize - 1
	}
	if end < start {
		logger.Warn(
			"Requested end is before start after adjustment",
			"filePath", absPath,
			"start", start,
			"end", end,
		)
		c.AbortWithStatus(http.StatusRequestedRangeNotSatisfiable)
		c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		return
	}

	if start == 0 && end == fileSize-1 {
		logger.Info("Full file request", "filePath", absPath)
		streamFullFile(c, file, fileInfo, start, end, absPath)
	} else {
		logger.Info("Partial file request", "filePath", absPath, "start", start, "end", end)
		streamPartialFile(c, file, fileInfo, start, end, absPath)
	}
}

// getFileFromCacheOrOpen retrieves a file handle from the cache or opens and caches it.
// It assumes the caller holds the per-file mutex for filePath.
// It returns the file handle, a boolean indicating if it was retrieved from cache, and an error.
func getFileFromCacheOrOpen(filePath string) (*os.File, bool, error) {
	startTime := time.Now()

	fileCache.Lock()
	if entry, exists := fileCache.cache[filePath]; exists {
		if entry.file == nil {
			logger.Error("Cached file handle is nil in getFileFromCacheOrOpen", "filePath", filePath)
			delete(fileCache.cache, filePath)
			fileCache.Unlock()
			// If cached entry is invalid, proceed to open and cache a new one
			return openAndCacheFile(filePath, startTime)
		}
		entry.refCount++
		fileCache.Unlock()

		logger.Debug(
			"Reused cached file",
			"filePath", filePath,
			"refCount", entry.refCount,
			"elapsed", time.Since(startTime),
		)
		return entry.file, true, nil // Indicate it was from cache
	}

	if len(fileCache.cache) >= maxCachedFiles {
		var lruPath string
		var lruTime time.Time
		for path, entry := range fileCache.cache {
			// We hold the global fileCache.Lock() here, checking entry.refCount is safe
			// We do NOT attempt to acquire individual fileMu here to avoid deadlock during iteration.
			if entry.refCount == 0 && (lruPath == "" || entry.lastUsed.Before(lruTime)) {
				lruPath = path
				lruTime = entry.lastUsed
			}
		}
		if lruPath != "" {
			// Found an LRU candidate. Before closing, we need to acquire its mutex.
			// This requires releasing the global fileCache.Lock() first.
			entryToEvict := fileCache.cache[lruPath] // Keep reference while unlocked
			fileCache.Unlock()                       // Release global cache lock to acquire per-file mutex

			muToEvict, loaded := fileMu.Load(lruPath)
			if loaded {
				mutexToEvict := muToEvict.(*sync.Mutex)
				mutexToEvict.Lock() // Acquire mutex of the file being evicted

				fileCache.Lock() // Re-acquire global cache lock to modify map
				// Re-check condition under both locks, as state might have changed
				if entry, exists := fileCache.cache[lruPath]; exists &&
					entry == entryToEvict &&
					entry.refCount == 0 &&
					time.Since(entry.lastUsed) > fileCacheEntryTTL {
					if err := entry.file.Close(); err != nil {
						logger.Error(
							"Error closing evicted file during getOrOpen",
							"filePath", lruPath,
							"error", err,
						)
					}
					delete(fileCache.cache, lruPath)
					fileMu.Delete(lruPath) // Remove the mutex from the map
					logger.Info(
						"Evicted cached file and removed mutex during getOrOpen",
						"filePath", lruPath,
					)

					contentCache.Lock()
					delete(contentCache.cache, lruPath)
					contentCache.Unlock()
					logger.Debug("Cleared content cache during eviction", "filePath", lruPath)
				}
				fileCache.Unlock() // Release global cache lock

				mutexToEvict.Unlock() // Release mutex of the file being evicted
			} else {
				// Should not happen if lruPath was in fileCache, but handle defensively
				fileCache.Lock()                 // Re-acquire global lock if we didn't find mutex
				delete(fileCache.cache, lruPath) // Just remove from cache if mutex is gone
				fileCache.Unlock()
				logger.Warn("Evicted file from cache but mutex was missing", "filePath", lruPath)
			}
			// After potentially evicting, the cache might have space. Proceed to open and cache the requested file.
			return openAndCacheFile(filePath, startTime)

		} else {
			// Cache is full, but no suitable entry to evict (all in use or recently used).
			// For now, we'll just open the file without adding to cache.
			// *** IMPLEMENTATION OF SUGGESTION 4: Lower log level for cache full warnings ***
			logger.Debug(
				"File cache full and no entries to evict",
				"filePath", filePath,
				"cacheSize", len(fileCache.cache),
				"maxCachedFiles", maxCachedFiles,
			)
			fileCache.Unlock() // Release global cache lock

			file, err := os.Open(filePath)
			if err != nil {
				return nil, false, err // File not found or other open error
			}
			logger.Debug(
				"File opened directly (cache full)",
				"filePath", filePath,
				"elapsed", time.Since(startTime),
			)
			return file, false, nil // Return false as it's not cached
		}
	}

	// Cache not full or no eviction needed, open and cache the requested file.
	return openAndCacheFile(filePath, startTime)
}

// openAndCacheFile opens a file, preloads content synchronously, adds it to the cache, and preloads content.
// It assumes the caller holds the per-file mutex for filePath.
// It returns the file handle, a boolean indicating if it was newly cached, and an error.
func openAndCacheFile(filePath string, startTime time.Time) (*os.File, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, false, err
	}

	// *** IMPLEMENTATION OF SUGGESTION 2: Synchronous preloading ***
	// Preload content immediately after opening the file
	preloadContentStartTime := time.Now()
	preloadFileContent(filePath, file)
	logger.Debug(
		"Synchronous preloading completed",
		"filePath", filePath,
		"elapsed", time.Since(preloadContentStartTime),
	)

	fileCache.Lock()
	// Check if another Goroutine already added it while we were opening and preloading
	if entry, exists := fileCache.cache[filePath]; exists {
		if closeErr := file.Close(); closeErr != nil {
			logger.Error(
				"Error closing file opened during cache race",
				"filePath", filePath,
				"error", closeErr,
			)
		}
		entry.refCount++
		fileCache.Unlock()
		logger.Debug(
			"Cache race: Used existing cached file",
			"filePath", filePath,
			"refCount", entry.refCount,
		)
		return entry.file, true, nil
	}

	// Add to cache
	entry := &fileEntry{
		file:     file,
		refCount: 1,
		lastUsed: time.Now(),
	}
	fileCache.cache[filePath] = entry
	fileCache.Unlock()

	logger.Debug("File opened and cached", "filePath", filePath, "elapsed", time.Since(startTime))
	return file, true, nil
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
		logger.Warn(
			"Invalid range header format, falling back to full file",
			"rangeHeader", rangeHeader,
			"elapsed", time.Since(startTime),
		)
		return 0, fileSize - 1
	}

	rangeParts := strings.SplitN(ranges[1], "-", 2)
	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil || start < 0 {
		logger.Warn(
			"Invalid start range, falling back to full file",
			"start", rangeParts[0],
			"fileSize", fileSize,
			"elapsed", time.Since(startTime),
		)
		return 0, fileSize - 1
	}

	var end int64
	if rangeParts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(rangeParts[1], 10, 64)
		if err != nil || end < start {
			logger.Warn(
				"Invalid end range, adjusting to file end",
				"end", rangeParts[1],
				"fileSize", fileSize,
				"elapsed", time.Since(startTime),
			)
			end = fileSize - 1
		}
	}

	if end >= fileSize {
		end = fileSize - 1
	}

	logger.Debug("Range header parsed", "start", start, "end", end, "elapsed", time.Since(startTime))
	return start, end
}

func streamFullFile(c *gin.Context, file *os.File, fileInfo os.FileInfo, start, end int64, filePath string) {
	fileSize := fileInfo.Size()
	contentType := getFileContentType(fileInfo)

	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Accept-Ranges", "bytes")

	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" || start != 0 || end != fileSize-1 {
		c.Status(http.StatusPartialContent)
		c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		c.Writer.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	} else {
		c.Status(http.StatusOK)
		c.Writer.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
	}
	c.Writer.Flush()

	logger.Info(
		"Streaming full file (adjusted by range if needed)",
		"fileName", fileInfo.Name(),
		"requestHeaders", c.Request.Header,
		"responseHeaders", c.Writer.Header(),
		"fileSize", fileSize,
		"start", start,
		"end", end,
	)
	streamFile(c, file, start, end, filePath)
}

func streamPartialFile(c *gin.Context, file *os.File, fileInfo os.FileInfo, start, end int64, filePath string) {
	fileSize := fileInfo.Size()
	contentType := getFileContentType(fileInfo)
	contentLength := end - start + 1

	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Writer.Header().Set("Accept-Ranges", "bytes")
	c.Status(http.StatusPartialContent)
	c.Writer.Flush()

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
	streamFile(c, file, start, end, filePath)
}

// streamFile handles the actual reading and writing of file data.
// It assumes the caller holds the per-file mutex for filePath.
func streamFile(c *gin.Context, file *os.File, start, end int64, filePath string) {
	startTime := time.Now()
	bufferSize := 2 * 1024 * 1024
	if start != 0 {
		bufferSize = 4 * 1024 * 1024
	}

	bufferGetTime := time.Now()
	buffer := bufferPool.Get().([]byte)
	if cap(buffer) < bufferSize {
		buffer = make([]byte, bufferSize)
	} else {
		buffer = buffer[:bufferSize]
	}
	logger.Debug("Buffer acquired", "size", len(buffer), "elapsed", time.Since(bufferGetTime))
	defer bufferPool.Put(buffer)

	currentOffset := start
	bytesRemaining := end - start + 1
	writtenBytes := int64(0)
	chunkCount := 0

	// Utilize content cache for ranges within cached data
	contentCache.Lock()
	if cached, exists := contentCache.cache[filePath]; exists {
		// Determine if the requested range starts within the cached data
		if currentOffset < int64(len(cached)) {
			// Calculate the portion of the requested range that can be served from cache
			cacheServeStart := currentOffset        // Start from the requested offset
			cacheServeEnd := int64(len(cached)) - 1 // End at the limit of cached data
			if cacheServeEnd > end {
				cacheServeEnd = end // Do not serve beyond the requested end
			}
			cacheServeLength := cacheServeEnd - cacheServeStart + 1

			if cacheServeLength > 0 {
				logger.Debug(
					"Serving initial chunk from content cache",
					"startInCache", cacheServeStart,
					"endInCache", cacheServeEnd,
					"bytes", cacheServeLength,
				)
				// Serve from the cached data at the calculated offset
				_, writeErr := c.Writer.Write(cached[cacheServeStart : cacheServeEnd+1])
				contentCache.Unlock()

				if writeErr != nil {
					logger.Error("Client connection lost during cached write", "error", writeErr)
					return
				}
				c.Writer.Flush()

				// Update offsets and remaining bytes after serving from cache
				writtenBytes += cacheServeLength
				bytesRemaining -= cacheServeLength
				currentOffset += cacheServeLength
				logger.Debug(
					"Served from cache", "servedBytes",
					cacheServeLength,
					"remainingBytes", bytesRemaining,
					"newOffset", currentOffset,
				)

				// If the entire range was served from cache, we are done.
				if bytesRemaining == 0 {
					logger.Info(
						"File streaming completed (from cache)",
						"start", start,
						"end", end,
						"totalBytes", end-start+1,
						"totalElapsed", time.Since(startTime),
					)
					return
				}

				logger.Debug(
					"Continuing stream from file after partial serve from cache",
					"newStart", currentOffset,
				)
			} else {
				contentCache.Unlock()
			}
		} else {
			contentCache.Unlock()
		}
	} else {
		contentCache.Unlock()
	}

	// Seek to the current offset if needed. This Seek is protected by the per-file mutex held by the caller (Stream).
	// We only need to seek if bytesRemaining > 0 AND the file's current offset is not already at currentOffset.
	// Assuming a new file handle or handle from cache starts at offset 0, we need to seek if currentOffset > 0.
	if bytesRemaining > 0 && currentOffset > 0 {
		seekStartTime := time.Now()
		if _, err := file.Seek(currentOffset, io.SeekStart); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			logger.Error("Error seeking file", "offset", currentOffset, "error", err)
			return
		}
		logger.Debug("Seek completed", "offset", currentOffset, "elapsed", time.Since(seekStartTime))
	} else if bytesRemaining > 0 && currentOffset == 0 {
		// No seek needed if starting from the beginning of the file (offset 0) and bytes remaining.
	} else if bytesRemaining == 0 {
		// No seek needed if all bytes were served from cache.
	}

	// Continue streaming from the file
	for bytesRemaining > 0 {
		chunkStartTime := time.Now()
		chunkCount++
		readSize := int64(len(buffer))
		if bytesRemaining < readSize {
			readSize = bytesRemaining
		}

		readStartTime := time.Now()
		n, err := file.Read(buffer[:readSize])
		if err != nil {
			if err == io.EOF {
				// logger.Debug("Read EOF", "remainingBytes", bytesRemaining, "elapsed", time.Since(readStartTime)) // Reduce logging verbosity
				break
			}
			c.AbortWithStatus(http.StatusInternalServerError)
			logger.Error(
				"Error reading file",
				"error", err,
				"remainingBytes", bytesRemaining,
				"elapsed", time.Since(readStartTime),
			)
			return
		}
		if n == 0 {
			break
		}

		writeStartTime := time.Now()
		_, writeErr := c.Writer.Write(buffer[:n])
		if writeErr != nil {
			logger.Error(
				"Client connection lost",
				"error", writeErr,
				"remainingBytes", bytesRemaining,
				"writtenBytes", writtenBytes,
				"elapsed", time.Since(writeStartTime),
			)
			return
		}

		writtenBytes += int64(n)
		bytesRemaining -= int64(n)
		currentOffset += int64(n)

		flushStartTime := time.Now()
		if writtenBytes%int64(flushChunkSize) == 0 || bytesRemaining == 0 {
			c.Writer.Flush()
			logger.Debug(
				"Flush executed",
				"writtenBytes", writtenBytes,
				"remainingBytes", bytesRemaining,
				"elapsed", time.Since(flushStartTime),
			)
		}

		if chunkCount%10 == 0 {
			logger.Debug(
				"Chunk processed from file",
				"chunk", chunkCount,
				"bytesRead", n,
				"remaining", bytesRemaining,
				"currentOffset", currentOffset,
				"elapsed", time.Since(chunkStartTime),
			)
		}
	}

	finalFlushStartTime := time.Now()
	c.Writer.Flush()
	logger.Debug(
		"Final flush executed",
		"elapsed", time.Since(finalFlushStartTime),
	)

	totalStreamed := (end - start + 1) - bytesRemaining
	logger.Info(
		"File streaming completed",
		"start", start,
		"end", end,
		"totalBytesRequested", end-start+1,
		"totalBytesStreamed", totalStreamed,
		"totalElapsed", time.Since(startTime),
	)
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
