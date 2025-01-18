package streamer

import (
	"PiliPili_Backend/config"
	"PiliPili_Backend/logger"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

// Remote handles streaming a file and checking for valid Range requests.
func Remote(c *gin.Context) {
	logger.Info("Start remote stream")

	signature := c.Query("signature")
	path := c.Query("path")

	mediaId, expireAt, err := authenticate(c, signature)
	if err != nil {
		logger.Error("Authentication failed", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	beijingTime := expireAt.In(time.FixedZone("CST", 8*3600))
	expireAtFormatted := beijingTime.Format("2006-01-02 15:04:05")
	logger.Info(
		"Authentication successful",
		"path", path,
		"mediaId", mediaId,
		"expireAt", expireAtFormatted,
	)

	// File info
	filepath := config.GetConfig().StorageBasePath + path
	Stream(c, filepath)
}

// authenticate verifies the provided signature by decrypting and validating its contents.
func authenticate(c *gin.Context, signature string) (mediaId string, expireAt time.Time, err error) {
	sigInstance, initErr := GetSignatureInstance()
	if initErr != nil {
		logger.Error("Signature instance is not initialized", "error", initErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return "", time.Time{}, initErr
	}

	data, decryptErr := sigInstance.Decrypt(signature)
	if decryptErr != nil {
		logger.Error("Failed to decrypt signature", "error", decryptErr)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid signature"})
		return "", time.Time{}, decryptErr
	}

	mediaIdValue, mediaIdExists := data["mediaId"].(string)
	expireAtValue, expireAtExists := data["expireAt"].(float64)
	if !mediaIdExists || !expireAtExists {
		logger.Error("Invalid decrypted data: missing required fields", "data", data)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid signature structure"})
		return "", time.Time{}, errors.New("invalid signature structure")
	}

	if mediaIdValue == "" {
		logger.Error("Authentication failed: mediaId is empty")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "mediaId is empty"})
		return "", time.Time{}, errors.New("mediaId is empty")
	}

	expireAt = time.Unix(int64(expireAtValue), 0)
	if expireAt.Before(time.Now().UTC()) {
		logger.Error("Authentication failed: signature expired", "expireAt", expireAt)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Signature has expired"})
		return "", time.Time{}, errors.New("signature has expired")
	}

	return mediaIdValue, expireAt, nil
}
