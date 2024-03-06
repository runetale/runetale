#!/bin/bash
go build -o runetale ./cmd/runetale/runetale.go
sudo ./runetale login -signal-host=$SIGNAL_HOST \
    -server-host=$SERVER_HOST \
    -signal-port=$SIGNAL_PORT \
    -server-port=$SERVER_PORT \
    -debug=$IS_DEBUG \
