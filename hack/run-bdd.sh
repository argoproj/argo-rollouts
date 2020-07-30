#!/bin/sh
set -eu

cd test/bdd
godog *.feature
