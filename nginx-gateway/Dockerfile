FROM hashicorp/consul-template:alpine AS consul-template

FROM nginx:alpine
COPY --from=consul-template /bin/consul-template /usr/local/bin/consul-template
COPY templates/nginx.conf.ctmpl /etc/consul-templates/nginx.conf.ctmpl
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh
EXPOSE 8000
ENTRYPOINT ["/docker-entrypoint.sh"]