package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	mathrand "math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/S-Corkum/mcp-server/internal/api"
	"github.com/S-Corkum/mcp-server/internal/aws"
	"github.com/S-Corkum/mcp-server/internal/cache"
	"github.com/S-Corkum/mcp-server/internal/config"
	"github.com/S-Corkum/mcp-server/internal/core"
	"github.com/S-Corkum/mcp-server/internal/database"
	"github.com/S-Corkum/mcp-server/internal/metrics"
	
	// Import PostgreSQL driver
	_ "github.com/lib/pq"
)

func main() {
	// Initialize secure random seed
	initSecureRandom()
	
	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error

	// Initialize configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	
	// Validate critical configuration
	if err := validateConfiguration(cfg); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Initialize metrics
	metricsClient := metrics.NewClient(cfg.Metrics)
	defer metricsClient.Close()

	// Check if IRSA is enabled (IAM Roles for Service Accounts)
	if aws.IsIRSAEnabled() {
		log.Println("IRSA (IAM Roles for Service Accounts) is enabled for AWS services")
		log.Println("AWS Role ARN:", os.Getenv("AWS_ROLE_ARN"))
		log.Println("AWS Web Identity Token File:", os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE"))
	} else {
		log.Println("IRSA not detected, will use standard AWS credential provider chain if IAM auth is enabled")
	}

	// Prepare database config with AWS integration if needed
	var db *database.Database
	var dbConfig database.Config
	if cfg.AWS.RDS.UseIAMAuth && aws.IsIRSAEnabled() {
		log.Println("Using IAM authentication for RDS")
		useAWS := true
		useIAM := true
		dbConfig = database.Config{
			Driver:         "postgres",
			UseAWS:         &useAWS,
			UseIAM:         &useIAM,
			RDSConfig:      &cfg.AWS.RDS,
			MaxOpenConns:   cfg.AWS.RDS.MaxOpenConns,
			MaxIdleConns:   cfg.AWS.RDS.MaxIdleConns,
			ConnMaxLifetime: cfg.AWS.RDS.ConnMaxLifetime,
		}
	} else {
		dbConfig = cfg.Database
	}

	// Initialize database
	db, err = database.NewDatabase(ctx, dbConfig)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Prepare cache config with AWS integration if needed
	var cacheClient cache.Cache
	var cacheConfig cache.RedisConfig
	if cfg.AWS.ElastiCache.UseIAMAuth && aws.IsIRSAEnabled() {
		log.Println("Using IAM authentication for ElastiCache")
		cacheConfig = cache.RedisConfig{
			Type:              "redis_cluster",
			UseAWS:            true,
			ClusterMode:       cfg.AWS.ElastiCache.ClusterMode,
			ElastiCacheConfig: &cfg.AWS.ElastiCache,
			MaxRetries:        cfg.AWS.ElastiCache.MaxRetries,
			DialTimeout:       cfg.AWS.ElastiCache.DialTimeout,
			ReadTimeout:       cfg.AWS.ElastiCache.ReadTimeout,
			WriteTimeout:      cfg.AWS.ElastiCache.WriteTimeout,
			PoolSize:          cfg.AWS.ElastiCache.PoolSize,
			MinIdleConns:      cfg.AWS.ElastiCache.MinIdleConnections,
			PoolTimeout:       cfg.AWS.ElastiCache.PoolTimeout,
		}
	} else {
		cacheConfig = cfg.Cache
	}

	// Initialize cache
	cacheClient, err = cache.NewCache(ctx, cacheConfig)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}
	defer cacheClient.Close()

	// Initialize engine
	var engine *core.Engine
	engine, err = core.NewEngine(ctx, cfg.Engine, db, cacheClient, metricsClient)
	if err != nil {
		log.Fatalf("Failed to initialize core engine: %v", err)
	}
	defer engine.Shutdown(ctx)

	// Convert interfaces.APIConfig to api.Config
	apiConfig := api.Config{
		ListenAddress: cfg.API.ListenAddress,
		ReadTimeout:   cfg.API.ReadTimeout,
		WriteTimeout:  cfg.API.WriteTimeout,
		IdleTimeout:   cfg.API.IdleTimeout,
		EnableCORS:    cfg.API.EnableCORS,
		EnableSwagger: cfg.API.EnableSwagger,
		TLSCertFile:   cfg.API.TLSCertFile,
		TLSKeyFile:    cfg.API.TLSKeyFile,
		Auth: api.AuthConfig{
			JWTSecret: cfg.API.Auth.JWTSecret,
			APIKeys:   cfg.API.Auth.APIKeys,
		},
		RateLimit: api.RateLimitConfig{
			Enabled:     cfg.API.RateLimit.Enabled,
			Limit:       cfg.API.RateLimit.Limit,
			Period:      time.Minute, // Default value
			BurstFactor: 3,           // Default value
		},
		// Default values for other fields
		Versioning: api.VersioningConfig{
			Enabled:           true,
			DefaultVersion:    "1.0",
			SupportedVersions: []string{"1.0"},
		},
		Performance: api.DefaultConfig().Performance,
	}
	
	// Initialize API server
	server := api.NewServer(engine, apiConfig)

	// Determine the correct port based on environment
	port := cfg.GetListenPort()
	log.Printf("Configured to listen on port %d", port)

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on %s", cfg.API.ListenAddress)
		
		// If we're in production and TLS is configured, use HTTPS
		if cfg.IsProduction() && cfg.API.TLSCertFile != "" && cfg.API.TLSKeyFile != "" {
			log.Println("Starting server with TLS (HTTPS)")
			if err := server.StartTLS(cfg.API.TLSCertFile, cfg.API.TLSKeyFile); err != nil {
				log.Fatalf("Failed to start server with TLS: %v", err)
			}
		} else {
			// Otherwise use HTTP
			if err := server.Start(); err != nil {
				log.Fatalf("Failed to start server: %v", err)
			}
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Received shutdown signal")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
	defer shutdownCancel()

	// Shutdown API server first
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("API server shutdown error: %v", err)
	}

	log.Println("Server stopped gracefully")
}

// initSecureRandom initializes the math/rand package with a secure seed
func initSecureRandom() {
	// Generate a secure random seed using crypto/rand
	max := big.NewInt(int64(1) << 62)
	val, err := rand.Int(rand.Reader, max)
	if err != nil {
		// If we can't get a secure seed, use time as a fallback
		log.Printf("Warning: unable to generate secure random seed: %v", err)
		// Use the global Seed function which works across all supported Go versions
		mathrand.Seed(time.Now().UnixNano())
		return
	}
	
	// Seed the global random generator with our secure random value
	mathrand.Seed(val.Int64())
	log.Println("Initialized secure random generator")
}

// validateConfiguration validates critical configuration settings
func validateConfiguration(cfg *config.Config) error {
	// Check database configuration
	if cfg.Database.DSN == "" && (cfg.Database.Host == "" || cfg.Database.Port == 0 || cfg.Database.Database == "") {
		// If we're using AWS RDS with IAM authentication, we don't need DSN or database credentials
		if !(cfg.AWS.RDS.UseIAMAuth && cfg.AWS.RDS.Host != "") {
			return fmt.Errorf("invalid database configuration: DSN or host/port/database must be provided")
		}
	}
	
	// Validate API configuration
	if cfg.API.ReadTimeout == 0 || cfg.API.WriteTimeout == 0 || cfg.API.IdleTimeout == 0 {
		return fmt.Errorf("invalid API timeouts: must be greater than 0")
	}
	
	// Check webhook secrets if webhooks are enabled
	if cfg.API.Webhooks.GitHub.Enabled && cfg.API.Webhooks.GitHub.Secret == "" {
		log.Println("Warning: GitHub webhooks enabled without a secret - consider adding a secret for security")
	}
	
	return nil
}

// S3 functionality has been removed in this version
