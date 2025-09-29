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
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RunServer starts the podboard web server.
func RunServer(address, domain string, logger *zap.Logger) error {
	gin.SetMode(gin.ReleaseMode)

	domain = getDomainFromEnvOrDefault(domain)
	fmt.Printf("Domain: %s\n", domain)

	// Initialize services
	kubeConfigService := NewKubeConfigService(logger)
	podService := NewPodService(kubeConfigService, logger)

	// Set up router
	router := setupRouter()

	// Setup routes
	setupAPIRoutes(router, podService, kubeConfigService)
	SetupUIRoutes(router)

	logger.Info("Server starting", zap.String("address", address))

	runErr := router.Run(address)
	if runErr != nil {
		return fmt.Errorf("failed to start server: %w", runErr)
	}

	return nil
}

func getDomainFromEnvOrDefault(domain string) string {
	if domain == "" {
		domain = os.Getenv("DOMAIN")
	}
	return domain
}

func setupRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	return router
}

func setupAPIRoutes(router *gin.Engine, podService *PodService, kubeConfigService *KubeConfigService) {
	api := router.Group("/api")

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
			c.JSON(500, gin.H{"error": err.Error()})
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
			c.JSON(500, gin.H{"error": err.Error()})
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
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"pods": pods})
	})

	// Delete pod endpoint
	api.DELETE("/pods/:namespace/:name", func(c *gin.Context) {
		clusterName := c.Query("cluster")
		namespace := c.Param("namespace")
		podName := c.Param("name")

		err := podService.DeletePod(c.Request.Context(), clusterName, namespace, podName)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "Pod deleted successfully"})
	})
}

