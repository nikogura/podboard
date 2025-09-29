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
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nikogura/podboard/pkg/ui"
)

// SetupUIRoutes configures the UI routes for serving the embedded frontend.
func SetupUIRoutes(router *gin.Engine) {
	// Get the subdirectory containing the built UI files from the ui package
	uiFS, err := fs.Sub(ui.Files, "dist")
	if err != nil {
		// If UI files don't exist (development), serve a placeholder
		router.GET("/", func(c *gin.Context) {
			c.HTML(http.StatusOK, "placeholder.html", gin.H{
				"title": "podboard - Kubernetes Pod Dashboard",
			})
		})
		return
	}

	// Serve favicon.ico directly from root
	router.GET("/favicon.ico", func(c *gin.Context) {
		faviconData, faviconErr := fs.ReadFile(uiFS, "favicon.ico")
		if faviconErr != nil {
			c.Status(http.StatusNotFound)
			return
		}

		c.Header("Content-Type", "image/x-icon")
		c.Header("Cache-Control", "public, max-age=86400")
		c.Data(http.StatusOK, "image/x-icon", faviconData)
	})

	// Serve static files with appropriate caching headers
	router.StaticFS("/static", http.FS(uiFS))

	// Serve the main application for all non-API routes
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Skip API routes
		if len(path) >= 4 && path[:4] == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"error": "API endpoint not found"})
			return
		}

		// Skip health check
		if path == "/health" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}

		// For all other routes, serve the main HTML file (SPA routing)
		indexFile, openErr := uiFS.Open("index.html")
		if openErr != nil {
			c.String(http.StatusInternalServerError, "UI not available")
			return
		}
		defer func() { _ = indexFile.Close() }()

		indexContent, readFileErr := fs.ReadFile(uiFS, "index.html")
		if readFileErr != nil {
			c.String(http.StatusInternalServerError, "UI not available")
			return
		}

		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.String(http.StatusOK, string(indexContent))
	})
}
