# BusyBox/musl userland on purpose: no GNU coreutils/procps, no PAM. Shows what the product assumes of a remote.
FROM alpine:3.22
RUN apk add --no-cache openssh-server openssh-sftp-server sudo \
    && mkdir -p /run/sshd && rm -f /etc/ssh/ssh_host_* \
    && adduser -D -s /bin/ash tester && echo 'tester:test-password' | chpasswd \
    && echo 'export LANG=C.UTF-8' > /etc/profile.d/lang.sh
COPY sshd_config /etc/ssh/sshd_config
COPY entrypoint.sh /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
