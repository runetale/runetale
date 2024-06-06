#!/bin/bash
go build -o runetaled ./cmd/runetaled/runetaled.go
sudo ./runetaled up -signal-host=$SIGNAL_HOST \
    -server-host=$SERVER_HOST \
    -signal-port=$SIGNAL_PORT \
    -server-port=$SERVER_PORT \
    -daemon=$IS_DEAMON \
    -debug=$IS_DEBUG