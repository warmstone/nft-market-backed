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
	Port           int
	AllowedOrigins []string `mapstructure:"allowed_origins"`
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

func (d DatabaseConfig) MigrationURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		d.User, d.Password, d.Host, d.Port, d.Name,
	)
}

type RedisConfig struct {
	Addr string
}

type EthereumConfig struct {
	RPCURL                   string `mapstructure:"rpc_url"`
	WSURL                    string `mapstructure:"ws_url"`
	ChainID                  int64  `mapstructure:"chain_id"`
	ExchangeAddress          string `mapstructure:"exchange_address"`
	ProtocolManagerAddress   string `mapstructure:"protocol_manager_address"`
	CollectionManagerAddress string `mapstructure:"collection_manager_address"`
	RoyaltyManagerAddress    string `mapstructure:"royalty_manager_address"`
	ConfirmationBlocks       int64  `mapstructure:"confirmation_blocks"`
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
