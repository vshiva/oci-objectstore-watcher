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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/urfave/cli.v1"

	"github.com/fnproject/oci-objectstore-watcher/ociobjectstorewatcherpb"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/pkg/errors"
	"github.com/wercker/auth/middleware"
	"github.com/wercker/pkg/conf"
	"github.com/wercker/pkg/log"
	"github.com/wercker/pkg/trace"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var gatewayCommand = cli.Command{
	Name:   "gateway",
	Usage:  "Start gRPC gateway",
	Action: gatewayAction,
	Flags:  append(gatewayFlags, conf.TraceFlags()...),
}

var gatewayFlags = []cli.Flag{
	cli.IntFlag{
		Name:   "port",
		Value:  43404,
		EnvVar: "HTTP_PORT",
	},
	cli.StringFlag{
		Name:   "host",
		Value:  "localhost:43403",
		EnvVar: "GRPC_HOST",
	},
}

var gatewayAction = func(c *cli.Context) error {
	log.Info("Starting oci-objectstore-watcher gateway")

	log.Debug("Parsing gateway options")
	o, err := parseGatewayOptions(c)
	if err != nil {
		log.WithError(err).Error("Unable to validate arguments")
		return errorExitCode
	}

	tracer, err := getTracer(o.TraceOptions, "oci-objectstore-watcher-gw", o.Port)
	if err != nil {
		log.WithError(err).Error("Unable to create a tracer")
		return errorExitCode
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{EmitDefaults: true})) // grpc-gateway

	// The following handlers will be called in reversed order (ie. bottom to top)
	var handler http.Handler
	handler = middleware.AuthTokenMiddleware(mux)   // authentication middleware
	handler = trace.HTTPMiddleware(handler, tracer) // opentracing + expose trace ID

	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(otgrpc.OpenTracingClientInterceptor(tracer)), // opentracing (outgoing)
	}

	err = ociobjectstorewatcherpb.RegisterOciObjectstoreWatcherHandlerFromEndpoint(ctx, mux, o.Host, opts)
	if err != nil {
		log.WithError(err).Error("Unable to register handler from Endpoint")
		return errorExitCode
	}

	errc := make(chan error, 2)

	// Shutdown on SIGINT, SIGTERM
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	s := &http.Server{
		Addr:    fmt.Sprintf(":%d", o.Port),
		Handler: handler,
	}
	// Start Gateway server in separate goroutine
	go func() {
		log.WithField("port", o.Port).Info("Starting server")
		err := s.ListenAndServe()
		errc <- errors.Wrap(err, "gateway returned an error")
	}()

	err = <-errc
	log.WithError(err).Info("Shutting down")

	// Gracefully shutdown the Gateway server
	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = s.Shutdown(ctx)
	if err != nil {
		log.WithError(err).Error("An error happened while shutting down")
	}
	return nil
}

type gatewayOptions struct {
	*conf.TraceOptions

	Port int
	Host string
}

func parseGatewayOptions(c *cli.Context) (*gatewayOptions, error) {
	traceOptions := conf.ParseTraceOptions(c)

	port := c.Int("port")
	if !validPortNumber(port) {
		return nil, fmt.Errorf("Invalid port number: %d", port)
	}

	return &gatewayOptions{
		TraceOptions: traceOptions,

		Port: port,
		Host: c.String("host"),
	}, nil
}
