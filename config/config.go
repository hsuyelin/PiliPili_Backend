// Package config handles loading and managing the application's configuration settings
package config

import (
	"github.com/spf13/viper"
)

// Config holds all configuration values.
type Config struct {
	Encipher        string // Key used for encryption and obfuscation
	StorageBasePath string // Prefix for storage paths, used to form full file paths
	Port            int    // Server port
	LogLevel        string // Log level (e.g., INFO, DEBUG, ERROR)
}

// globalConfig stores the loaded configuration.
var globalConfig Config

// Initialize loads the configuration from the provided config file and initializes the logger.
func Initialize(configFile string, loglevel string) error {
	viper.SetConfigType("yaml")

	if configFile != "" {
		viper.SetConfigFile(configFile)
	}

	if err := viper.ReadInConfig(); err != nil {
		globalConfig = Config{
			Encipher:        "",
			StorageBasePath: "",
			Port:            60002,
			LogLevel:        defaultLogLevel(loglevel),
		}
	} else {
		globalConfig = Config{
			Encipher:        viper.GetString("Encipher"),
			StorageBasePath: viper.GetString("StorageBasePath"),
			Port:            viper.GetInt("Server.port"),
			LogLevel:        getLogLevel(loglevel),
		}
	}

	return nil
}

// GetConfig returns the global configuration.
func GetConfig() Config {
	return globalConfig
}

// defaultLogLevel returns the default log level if no log level is specified.
func defaultLogLevel(loglevel string) string {
	if loglevel != "" {
		return loglevel
	}
	return "info"
}

// getLogLevel returns the log level from either the parameter or the config file.
func getLogLevel(loglevel string) string {
	if loglevel != "" {
		return loglevel
	}
	return viper.GetString("LogLevel")
}
