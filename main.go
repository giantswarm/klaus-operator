package main

import (
	"context"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/controller"
	"github.com/giantswarm/klaus-operator/internal/mcp"
	"github.com/giantswarm/klaus-operator/internal/oci"
	"github.com/giantswarm/klaus-operator/internal/resources"
	"github.com/giantswarm/klaus-operator/pkg/project"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(klausv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		mcpAddr              string
		enableLeaderElection bool
		klausImage           string
		gitCloneImage        string
		anthropicKeySecret   string
		anthropicKeyNs       string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&mcpAddr, "mcp-bind-address", ":9090", "The address the MCP server binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.StringVar(&klausImage, "klaus-image", "gsoci.azurecr.io/giantswarm/klaus:latest", "The Klaus container image to use for instances.")
	flag.StringVar(&gitCloneImage, "git-clone-image", resources.DefaultGitCloneImage, "The git clone image for workspace init containers.")
	flag.StringVar(&anthropicKeySecret, "anthropic-key-secret", "anthropic-api-key", "Name of the Secret containing the Anthropic API key.")
	flag.StringVar(&anthropicKeyNs, "anthropic-key-namespace", "", "Namespace of the Anthropic API key Secret (defaults to operator namespace).")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "klaus-operator.giantswarm.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Determine operator namespace for credential distribution.
	operatorNamespace := os.Getenv("POD_NAMESPACE")
	if operatorNamespace == "" {
		operatorNamespace = "klaus-system"
	}
	if anthropicKeyNs == "" {
		anthropicKeyNs = operatorNamespace
	}

	// Register field indexer for efficient MCP server reference lookups.
	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(ctx, &klausv1alpha1.KlausInstance{},
		controller.MCPServerRefIndexField, controller.IndexMCPServerRefs); err != nil {
		setupLog.Error(err, "unable to create field indexer", "field", controller.MCPServerRefIndexField)
		os.Exit(1)
	}

	// Create the OCI client for personality artifact resolution.
	ociClient := oci.NewClient(mgr.GetClient())

	// Set up the KlausInstance controller.
	if err := (&controller.KlausInstanceReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		Recorder:           mgr.GetEventRecorderFor("klausinstance-controller"),
		KlausImage:         klausImage,
		GitCloneImage:      gitCloneImage,
		AnthropicKeySecret: anthropicKeySecret,
		AnthropicKeyNs:     anthropicKeyNs,
		OperatorNamespace:  operatorNamespace,
		OCIClient:          ociClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KlausInstance")
		os.Exit(1)
	}

	// Set up the KlausMCPServer controller.
	if err := (&controller.KlausMCPServerReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorderFor("klausmcpserver-controller"),
		OperatorNamespace: operatorNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KlausMCPServer")
		os.Exit(1)
	}

	// Set up health checks.
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Add the MCP server as a manager runnable for graceful lifecycle management.
	mcpServer := mcp.NewServer(mgr.GetClient(), operatorNamespace, mcpAddr)
	if err := mgr.Add(mcpServer); err != nil {
		setupLog.Error(err, "unable to add MCP server to manager")
		os.Exit(1)
	}

	setupLog.Info("starting manager",
		"version", project.Version(),
		"gitSHA", project.GitSHA(),
		"buildTimestamp", project.BuildTimestamp(),
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
