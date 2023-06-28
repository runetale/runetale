#!/bin/sh
curl -L https://pkgs.runetale.com/debian/pgp-key.public | sudo apt-key add -
curl -L https://pkgs.runetale.com/debian/runetale.list | sudo tee /etc/apt/sources.list.d/runetale.list
