/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/inftyai/manta/agent/pkg/controller"
	"github.com/inftyai/manta/agent/pkg/server"
	"github.com/inftyai/manta/agent/pkg/task"
	api "github.com/inftyai/manta/api/v1alpha1"
)

var (
	setupLog logr.Logger
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	setupLog = ctrl.Log.WithName("Setup")

	cfg, err := config.GetConfig()
	if err != nil {
		setupLog.Error(err, "failed to get config")
		os.Exit(1)
	}

	setupLog.Info("Setting up manta-agent")

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = api.AddToScheme(scheme)

	mgr, err := manager.New(cfg, manager.Options{
		Scheme: scheme,
	})
	if err != nil {
		setupLog.Error(err, "failed to initialize the manager")
		os.Exit(1)
	}

	if err := controller.NewReplicationReconciler(
		mgr.GetClient(), mgr.GetScheme(),
	).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Model")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		<-sigs
		cancel()
	}()

	// Background tasks.
	task.BackgroundTasks(ctx, mgr.GetClient())

	// Run http server to receive sync requests.
	go server.Run(ctx)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
	}
}
