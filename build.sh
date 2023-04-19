#!/bin/bash

docker buildx build --push --platform=linux/amd64,linux/arm64 . -t poneding/exposer-controller -t registry.cn-hangzhou.aliyuncs.com/pding/exposer-controller -f Dockerfile