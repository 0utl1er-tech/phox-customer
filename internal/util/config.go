package util

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Config stores all configuration of the application.
// The values are read by viper from a config file or environment variable.
type Config struct {
	Environment              string `mapstructure:"ENV"`
	DBSource                 string `mapstructure:"DB_SOURCE"`
	ConnectServerAddress     string `mapstructure:"CONNECT_SERVER_ADDRESS"`
	JWTEnabled               bool   `mapstructure:"JWT_ENABLED"`
	JWTProjectID             string `mapstructure:"JWT_PROJECT_ID"`
	JWTIssuerURL             string `mapstructure:"JWT_ISSUER_URL"`
	JWTJwksURL               string `mapstructure:"JWT_JWKS_URL"`
	FirebaseAdminCredentials string `mapstructure:"FIREBASE_ADMIN_CREDENTIALS_FILE"`
}

// LoadConfig reads configuration from file or environment variables.
func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("app")
	viper.SetConfigType("env")

	// Enable automatic environment variable binding
	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&config)

	// Debug log to verify config values
	log.Debug().
		Str("jwks_url", config.JWTJwksURL).
		Str("issuer_url", config.JWTIssuerURL).
		Str("project_id", config.JWTProjectID).
		Bool("jwt_enabled", config.JWTEnabled).
		Msg("JWT configuration loaded")

	return
}
