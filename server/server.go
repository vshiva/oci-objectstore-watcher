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

package server

import (
	"github.com/fnproject/oci-objectstore-watcher/ociobjectstorewatcherpb"
	"github.com/fnproject/oci-objectstore-watcher/state"

	"golang.org/x/net/context"
)

// New Creates a new OciObjectstoreWatcherServer which implements ociobjectstorewatcherpb.OciObjectstoreWatcherServer.
func New(store state.Store) (*OciObjectstoreWatcherServer, error) {
	return &OciObjectstoreWatcherServer{
		store: store,
	}, nil
}

// OciObjectstoreWatcherServer implements ociobjectstorewatcherpb.OciObjectstoreWatcherServer.
type OciObjectstoreWatcherServer struct {
	store state.Store
}

// Action is a example implementation and should be replaced with an actual
// implementation.
func (s *OciObjectstoreWatcherServer) Action(ctx context.Context, req *ociobjectstorewatcherpb.ActionRequest) (*ociobjectstorewatcherpb.ActionResponse, error) {
	return &ociobjectstorewatcherpb.ActionResponse{}, nil
}

// Make sure that OciObjectstoreWatcherServer implements the ociobjectstorewatcherpb.OciObjectstoreWatcherService interface.
var _ ociobjectstorewatcherpb.OciObjectstoreWatcherServer = &OciObjectstoreWatcherServer{}
