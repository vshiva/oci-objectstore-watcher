#!/bin/bash
set -e

(

LOCAL=$(dirname $PWD)

if [ -e /var/run/docker.sock ]; then
  ROOT=${LOCAL//$GOPATH/\/go}
  protoc="docker run --rm \
    -u $(id -u $USER):$(id -g $USER) \
    -w $ROOT \
    -v $LOCAL:$ROOT \
    quay.io/wercker/protoc"
else
  ROOT=$LOCAL
  protoc="protoc \
    -I/usr/local/include
    -I.
    -I$GOPATH/src \
    -I./vendor \
    -I./vendor/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis"
fi

cd $LOCAL

echo "Generating gRPC server, gateway, swagger, flow"
$protoc --go_out=plugins=grpc:$ROOT/ociobjectstorewatcherpb \
        --grpc-gateway_out=logtostderr=true,request_context=true:$ROOT/ociobjectstorewatcherpb \
        --swagger_out=logtostderr=true:$ROOT/ociobjectstorewatcherpb \
        --flow_out=$ROOT/ociobjectstorewatcherpb \
        ociobjectstorewatcher.proto

)
