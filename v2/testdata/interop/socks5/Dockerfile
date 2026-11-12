# Dante SOCKS5 proxy: :1080 without auth, :1081 with username/password (user "proxy"/"proxy-pass").
FROM alpine:3.22
RUN apk add --no-cache dante-server \
    && adduser -D -s /sbin/nologin proxy && echo 'proxy:proxy-pass' | chpasswd
COPY open.conf auth.conf /etc/
COPY entrypoint.sh /entrypoint.sh
EXPOSE 1080 1081
ENTRYPOINT ["/entrypoint.sh"]
