/*
Copyright (c) 2024 Nik Ogura

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package podboard

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// jsonKeyError is the key API error responses carry their message under.
const jsonKeyError = "error"

// EnvReadOnly turns on read-only mode when set to a boolean true value. In
// read-only mode podboard is a viewer: the pod DELETE route is not registered
// and the UI does not render the Delete button.
const EnvReadOnly = "PODBOARD_READ_ONLY"

// RunServer starts the podboard web server. readOnly disables pod deletion;
// EnvReadOnly can turn read-only mode on as well, and either source wins.
func RunServer(address, domain string, readOnly bool, logger *zap.Logger) (err error) {
	gin.SetMode(gin.ReleaseMode)

	domain = getDomainFromEnvOrDefault(domain)
	fmt.Printf("Domain: %s\n", domain)

	readOnly, readOnlyErr := resolveReadOnly(readOnly, os.Getenv)
	if readOnlyErr != nil {
		err = fmt.Errorf("invalid read-only configuration: %w", readOnlyErr)
		return err
	}
	if readOnly {
		logger.Info("Read-only mode: pod deletion disabled and the DELETE route is not served")
	}

	// Initialize services
	informerConfig, configErr := InformerConfigFromEnv()
	if configErr != nil {
		err = fmt.Errorf("invalid informer configuration: %w", configErr)
		return err
	}

	kubeConfigService := NewKubeConfigService(logger)

	informerCtx, cancelInformers := context.WithCancel(context.Background())
	defer cancelInformers()

	informerManager := NewInformerManager(kubeConfigService.CreateClientForCluster, informerConfig, logger)
	informerManager.Start(informerCtx)
	defer informerManager.Stop()

	podService := NewPodService(kubeConfigService, informerManager, logger)

	// Set up router
	router := setupRouter()

	// Setup routes
	setupAPIRoutes(router, podService, kubeConfigService, readOnly)
	SetupUIRoutes(router)

	logger.Info("Server starting", zap.String("address", address))

	runErr := router.Run(address)
	if runErr != nil {
		err = fmt.Errorf("failed to start server: %w", runErr)
		return err
	}

	return err
}

func getDomainFromEnvOrDefault(domain string) (result string) {
	result = domain
	if result == "" {
		result = os.Getenv("DOMAIN")
	}
	return result
}

// resolveReadOnly combines the command line flag with EnvReadOnly. Either
// source asking for read-only mode wins, so neither can silently make a
// deployment writable that the other locked down. A value that is not a
// boolean is an error rather than a guess.
func resolveReadOnly(flag bool, getenv func(key string) (value string)) (readOnly bool, err error) {
	readOnly = flag

	raw := getenv(EnvReadOnly)
	if raw == "" {
		return readOnly, err
	}

	fromEnv, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		err = fmt.Errorf("%s=%q is not a boolean: %w", EnvReadOnly, raw, parseErr)
		return readOnly, err
	}

	readOnly = readOnly || fromEnv
	return readOnly, err
}

func setupRouter() (router *gin.Engine) {
	router = gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	return router
}

// setupAPIRoutes registers the JSON API. In read-only mode the pod DELETE
// route is left out entirely, so a delete request is a 404 rather than a
// permission check.
func setupAPIRoutes(router *gin.Engine, podService *PodService, kubeConfigService *KubeConfigService, readOnly bool) {
	api := router.Group("/api")

	// Server options the UI adapts to.
	api.GET("/config", func(c *gin.Context) {
		c.JSON(200, gin.H{"readOnly": readOnly})
	})

	// Clusters endpoint - only available when not running in cluster
	api.GET("/clusters", func(c *gin.Context) {
		if kubeConfigService.IsInCluster() {
			c.JSON(200, gin.H{
				"inCluster": true,
				"clusters":  []ClusterInfo{},
			})
			return
		}

		clusters, err := kubeConfigService.GetClusters()
		if err != nil {
			c.JSON(500, gin.H{jsonKeyError: err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"inCluster": false,
			"clusters":  clusters,
		})
	})

	api.GET("/namespaces", func(c *gin.Context) {
		clusterName := c.Query("cluster")
		namespaces, err := podService.GetNamespaces(c.Request.Context(), clusterName)
		if err != nil {
			c.JSON(500, gin.H{jsonKeyError: err.Error()})
			return
		}
		c.JSON(200, gin.H{"namespaces": namespaces})
	})

	api.GET("/pods", func(c *gin.Context) {
		clusterName := c.Query("cluster")
		namespace := c.DefaultQuery("namespace", "default")
		labelSelector := c.Query("labelSelector")
		pods, err := podService.GetPods(c.Request.Context(), clusterName, namespace, labelSelector)
		if err != nil {
			c.JSON(500, gin.H{jsonKeyError: err.Error()})
			return
		}
		c.JSON(200, gin.H{"pods": pods})
	})

	if readOnly {
		return
	}

	registerDeleteRoute(api, podService)
}

// registerDeleteRoute adds the one mutating endpoint podboard has.
func registerDeleteRoute(api *gin.RouterGroup, podService *PodService) {
	api.DELETE("/pods/:namespace/:name", func(c *gin.Context) {
		clusterName := c.Query("cluster")
		namespace := c.Param("namespace")
		podName := c.Param("name")

		err := podService.DeletePod(c.Request.Context(), clusterName, namespace, podName)
		if err != nil {
			c.JSON(500, gin.H{jsonKeyError: err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Pod deleted successfully"})
	})
}
