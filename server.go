//-----------------------------------------------------------------------------
// Copyright (c) 2017 Oracle and/or its affiliates.  All rights reserved.
// This program is free software: you can modify it and/or redistribute it
// under the terms of:
//
// (i)  the Universal Permissive License v 1.0 or at your option, any
//      later version (http://oss.oracle.com/licenses/upl); and/or
//
// (ii) the Apache License v 2.0. (http://www.apache.org/licenses/LICENSE-2.0)
//-----------------------------------------------------------------------------

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oracle/oci-go-sdk/common/auth"
	obstore "github.com/oracle/oci-go-sdk/objectstorage"

	"github.com/fnproject/oci-objectstore-watcher/ociobjectstorewatcherpb"
	"github.com/fnproject/oci-objectstore-watcher/server"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	grpcmw "github.com/mwitkow/go-grpc-middleware"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/wercker/pkg/conf"
	"github.com/wercker/pkg/health"
	"github.com/wercker/pkg/log"
	"github.com/wercker/pkg/trace"
	"google.golang.org/grpc"
	cli "gopkg.in/urfave/cli.v1"
)

var serverCommand = cli.Command{
	Name:   "server",
	Usage:  "start gRPC server",
	Action: serverAction,
	Flags:  append(serverFlags, conf.TraceFlags()...),
}

var serverFlags = []cli.Flag{
	cli.IntFlag{
		Name:   "port",
		Value:  43403,
		EnvVar: "PORT",
	},
	cli.IntFlag{
		Name:   "health-port",
		Value:  43405,
		EnvVar: "HEALTH_PORT",
	},
	cli.IntFlag{
		Name:   "metrics-port",
		Value:  43406,
		EnvVar: "METRICS_PORT",
	},
	cli.StringSliceFlag{
		Name:   "buckets",
		Usage:  "Object store buckets to watch",
		EnvVar: "OBJECTSTORE_BUCKETS",
	},
	cli.StringFlag{
		Name:   "namespace",
		Usage:  "Object store namespace",
		EnvVar: "OBJECTSTORE_NAMESPACE",
	},
	cli.StringFlag{
		Name:   "poll-interval",
		Usage:  "Polling interval to check bucket changes",
		Value:  "30s",
		EnvVar: "OBJECTSTORE_POLL_INTERVAL",
	},
	cli.StringFlag{
		Name:   "webhook-url",
		Usage:  "Webhook callback url at which changes are notified",
		EnvVar: "WEBHOOK_URL",
	},
}

var serverAction = func(c *cli.Context) error {
	log.Info("Starting oci-objectstore-watcher server")

	log.Debug("Parsing server options")
	o, err := parseServerOptions(c)
	if err != nil {
		log.WithError(err).Error("Unable to validate arguments")
		return errorExitCode
	}

	healthService := health.New()

	tracer, err := getTracer(o.TraceOptions, "oci-objectstore-watcher", o.Port)
	if err != nil {
		log.WithError(err).Error("Unable to create a tracer")
		return errorExitCode
	}

	cfgProvider, err := auth.InstancePrincipalConfigurationProvider()
	if err != nil {
		log.WithError(err).Error("Unable to get a Instance Principal Config Provider")
		return errorExitCode
	}
	client, err := obstore.NewObjectStorageClientWithConfigurationProvider(cfgProvider)
	if err != nil {
		log.WithError(err).Error("Unable to connect to object store")
		return errorExitCode
	}

	log.Debug("Creating server")
	server, err := server.New(client)
	if err != nil {
		log.WithError(err).Error("Unable to create server")
		return errorExitCode
	}

	// The following interceptors will be called in order (ie. top to bottom)
	interceptors := []grpc.UnaryServerInterceptor{
		trace.Interceptor(tracer),              // opentracing + expose trace ID
		grpc_prometheus.UnaryServerInterceptor, // prometheus
	}

	s := grpc.NewServer(grpcmw.WithUnaryServerChain(interceptors...))
	ociobjectstorewatcherpb.RegisterOciObjectstoreWatcherServer(s, server)
	grpc_prometheus.EnableHandlingTimeHistogram()
	grpc_prometheus.Register(s)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", o.Port))
	if err != nil {
		log.WithField("port", o.Port).WithError(err).Error("Failed to listen")
		return errorExitCode
	}

	errc := make(chan error, 4)

	// Shutdown on SIGINT, SIGTERM
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	// Start gRPC server
	go func() {
		log.WithField("port", o.Port).Info("Starting server")
		err := s.Serve(lis)
		errc <- errors.Wrap(err, "server returned an error")
	}()

	// Start health server
	go func() {
		log.WithField("port", o.HealthPort).Info("Starting health service")
		err := healthService.ListenAndServe(fmt.Sprintf(":%d", o.HealthPort))
		errc <- errors.Wrap(err, "health service returned an error")
	}()

	// Start metrics server
	go func() {
		log.WithField("port", o.MetricsPort).Info("Starting metrics server")
		http.Handle("/metrics", prometheus.Handler())
		errc <- http.ListenAndServe(fmt.Sprintf(":%d", o.MetricsPort), nil)
	}()

	err = <-errc
	log.WithError(err).Info("Shutting down")

	// Gracefully shutdown the health server
	healthService.Shutdown(context.Background())

	// Gracefully shutdown the gRPC server
	s.GracefulStop()

	return nil
}

type serverOptions struct {
	*conf.TraceOptions

	WebHookURL         url.URL
	Buckets            []string
	Namespace          string
	BucketPollInterval time.Duration

	Port        int
	HealthPort  int
	MetricsPort int
	StateStore  string
}

func parseServerOptions(c *cli.Context) (*serverOptions, error) {
	traceOptions := conf.ParseTraceOptions(c)

	port := c.Int("port")
	if !validPortNumber(port) {
		return nil, fmt.Errorf("invalid port number: %d", port)
	}

	healthPort := c.Int("health-port")
	if !validPortNumber(healthPort) {
		return nil, fmt.Errorf("invalid health-port number: %d", healthPort)
	}

	if healthPort == port {
		return nil, errors.New("health-port and port cannot be the same")
	}

	metricsPort := c.Int("metrics-port")
	if !validPortNumber(metricsPort) {
		return nil, fmt.Errorf("invalid metrics port number: %d", metricsPort)
	}

	if metricsPort == port {
		return nil, errors.New("metrics-port and port cannot be the same")
	}

	if metricsPort == healthPort {
		return nil, errors.New("metrics-port and health-port cannot be the same")
	}

	namespace := c.String("namespace")
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}

	webHook := c.String("webhook-url")
	if webHook == "" {
		return nil, errors.New("webhook-url is required")
	}
	webHookURL, err := url.Parse(webHook)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook-url - %v", err)
	}

	duration, err := time.ParseDuration(c.String("poll-interval"))
	if err != nil {
		return nil, fmt.Errorf("invalid poll interval - %v", err)
	}

	buckets := c.StringSlice("buckets")
	if len(buckets) == 0 {
		return nil, errors.New("Atleast one bucket must be specified")
	} else if len(buckets) > 10 {
		return nil, errors.New("A maximum of 10 buckets is supported")
	}

	return &serverOptions{
		TraceOptions:       traceOptions,
		Buckets:            buckets,
		Namespace:          namespace,
		WebHookURL:         *webHookURL,
		BucketPollInterval: duration,
		Port:               port,
		HealthPort:         healthPort,
		MetricsPort:        metricsPort,
		StateStore:         c.String("state-store"),
	}, nil
}
