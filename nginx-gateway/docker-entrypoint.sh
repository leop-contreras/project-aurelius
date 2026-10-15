#!/bin/sh
set -e

CONSUL_ADDR="${CONSUL_HTTP_ADDR:-consul:8500}"

consul-template \
  -consul-addr "${CONSUL_ADDR}" \
  -template "/etc/consul-templates/nginx.conf.ctmpl:/etc/nginx/nginx.conf" \
  -once

nginx -g 'daemon off;' &

exec consul-template \
  -consul-addr "${CONSUL_ADDR}" \
  -template "/etc/consul-templates/nginx.conf.ctmpl:/etc/nginx/nginx.conf:nginx -s reload"
