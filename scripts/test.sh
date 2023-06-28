#!/bin/bash
go build -o runetale ./cmd/runetale/runetale.go
sudo ./runetale login -signal-host=$SIGNAL_HOST \
    -server-host=$SERVER_HOST \
    -signal-port=$SIGNAL_PORT \
    -server-port=$SERVER_PORT \
    -debug=$IS_DEBUG \

go build -o runetaled ./cmd/runetaled/runetaled.go
sudo ./runetaled up -signal-host=$SIGNAL_HOST \
    -server-host=$SERVER_HOST \
    -signal-port=$SIGNAL_PORT \
    -server-port=$SERVER_PORT \
    -daemon=$IS_DEAMON \
    -debug=$IS_DEBUG \
