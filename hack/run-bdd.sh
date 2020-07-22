#!/bin/sh
set -euo pipefail

cd test/bdd
godog *.feature