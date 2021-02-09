/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/. */

package main

import (
	"flag"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	isindirv1alpha2 "github.com/isindir/sops-secrets-operator/api/v1alpha2"
	"github.com/isindir/sops-secrets-operator/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(isindirv1alpha2.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var requeueAfter int64

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
	var vaultAuth string
	var vaultRole string
	var vaultServer string
	var vaultTokenPath string

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Int64Var(&requeueAfter, "requeue-decrypt-after", 5, "Requeue failed reconciliation in minutes (min 1).")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Int64Var(&requeueAfter, "requeue-decrypt-after", 5, "Requeue failed decryption in minutes (min 1).")
	flag.StringVar(&vaultAuth, "vault-auth", "", "Vault Kubernetes authentication path.")
	flag.StringVar(&vaultRole, "vault-role", "", "Vault Kubernetes authentication role.")
	flag.StringVar(&vaultServer, "vault-server", "", "Vault API URL.")
	flag.StringVar(&vaultTokenPath, "vault-token-path", "/var/run/secrets/kubernetes.io/serviceaccount/token", "Service account token to use for Vault authentication.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ca57d051.github.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if requeueAfter < 1 {
		requeueAfter = 1
	}
	setupLog.Info(
		fmt.Sprintf(
			"SopsSecret reconciliation will be requeued after %d minutes after decryption failures",
			requeueAfter,
		),
	)

	if err = (&controllers.SopsSecretReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("SopsSecret"),
		Scheme:       mgr.GetScheme(),
		RequeueAfter: requeueAfter,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SopsSecret")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	stopCh := ctrl.SetupSignalHandler()

	if len(vaultRole) > 0 && len(vaultServer) > 0 && len(vaultTokenPath) > 0 && len(vaultAuth) > 0 {
		setupLog.Info("starting vault authenticator")

		vault, err := controllers.CreateVaultAuth(vaultServer, vaultAuth, vaultRole, vaultTokenPath)
		if err != nil {
			setupLog.Error(err, "unable to start vault authenticator")
			os.Exit(1)
		}

		go vault.StartAutoRenew(stopCh)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(stopCh); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
