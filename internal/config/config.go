package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type MigrationConfig struct {
	Enabled bool
}

type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Ethereum  EthereumConfig
	Auth      AuthConfig
	Migration MigrationConfig
}

type ServerConfig struct {
	Port int
}

type DatabaseConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, d.User, d.Password, d.Name,
	)
}

type RedisConfig struct {
	Addr string
}

type EthereumConfig struct {
	RPCURL                   string
	WSURL                    string
	ChainID                  int64
	ExchangeAddress          string
	ProtocolManagerAddress   string
	CollectionManagerAddress string
	RoyaltyManagerAddress    string
	ConfirmationBlocks       int64
}

type AuthConfig struct {
	JWTSecret    string        `mapstructure:"jwt_secret"`
	JWTExpiry    time.Duration `mapstructure:"jwt_expiry"`
	ChallengeTTL time.Duration `mapstructure:"challenge_ttl"`
}

func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}
