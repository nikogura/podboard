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

package cmd

import (
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/nikogura/podboard/pkg/podboard"
	"go.uber.org/zap"
)

//nolint:gochecknoglobals // Cobra boilerplate
var verbose bool

//nolint:gochecknoglobals // Cobra boilerplate
var logLevel string

//nolint:gochecknoglobals // Cobra boilerplate
var address string

//nolint:gochecknoglobals // Cobra boilerplate
var domain string


// rootCmd represents the base command when called without any subcommands.
//
//nolint:gochecknoglobals // Cobra boilerplate
var rootCmd = &cobra.Command{
	Use:   "podboard",
	Short: "Kubernetes Pod Dashboard",
	Long: `podboard is a web-based dashboard for monitoring Kubernetes pods.

It provides a real-time view of pod status across namespaces, similar to running
'kubectl get pod --watch'. The dashboard includes namespace filtering and configurable
refresh intervals.

The server provides:
- Web UI for viewing live pod status across namespaces
- Configurable refresh intervals (2s default)
- Namespace and cluster selector for filtering pods
- Real-time updates like 'kubectl get pod --watch'
- Regex support for label filtering (use =~ operator)

Kubernetes configuration:
- Uses in-cluster config when running in a pod
- Falls back to ~/.kube/config for local development
- NAMESPACE: Default namespace to monitor (default: default)

Example (local development - uses all defaults):
  go build && ./podboard
  ./podboard --bind-address=0.0.0.0:8080

Example (production deployment):
  ./podboard --bind-address=0.0.0.0:9999 --domain=podboard.example.com`,
	Run: func(cmd *cobra.Command, args []string) {
		// Create logger
		logger, err := zap.NewProduction()
		if err != nil {
			log.Fatalf("failed to create logger: %s", err)
		}
		defer func() {
			_ = logger.Sync() // Ignore error on logger sync in defer
		}()

		// Run the server
		err = podboard.RunServer(address, domain, logger)
		if err != nil {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

//nolint:gochecknoinits // Cobra boilerplate
func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "Info", "Log Level (Trace, Debug, Info, Warn, Error)")
	rootCmd.Flags().StringVarP(&address, "bind-address", "b", "0.0.0.0:9999", "Address (host and port) on which to listen")
	rootCmd.Flags().StringVarP(&domain, "domain", "d", "", "server domain name")
}
