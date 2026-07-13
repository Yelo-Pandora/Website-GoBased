#!/bin/sh
# Initializes ownership and modes for shared named volumes.

set -eu

chown -R 10001:10001 /volumes/orchestrator /volumes/lab-nginx
chmod 0750 /volumes/orchestrator
chmod 0755 /volumes/lab-nginx
