FROM ubuntu:16.04

MAINTAINER Jaime Soriano Pastor <jsoriano@tuenti.com>

RUN apt-get update && \
	apt-get install -y curl && \
	rm -rf /var/lib/apt/lists/*

COPY haproxy.cfg.tpl /etc/kube2lb/haproxy.cfg.tpl
COPY kube2lb /usr/local/bin/kube2lb
COPY entrypoint.sh /entrypoint.sh

ENV SYSLOG "127.0.0.1:514"
ENV HAPROXY_WRAPPER_CONTROL "127.0.0.1:15000"
ENV HAPROXY_MAXCONN "65536"
ENV HAPROXY_FRONTEND_MAXCONN "32768"
ENV HAPROXY_SERVER_MAXCONN "2048"
ENV HAPROXY_NBPROC "1"
ENV TEMPLATE /etc/kube2lb/haproxy.cfg.tpl

EXPOSE 80

CMD ["/entrypoint.sh"]
