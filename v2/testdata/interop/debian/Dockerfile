# Real OpenSSH on Debian/Ubuntu: PAM, keyboard-interactive, 2FA (google-authenticator scratch codes),
# plus the programs people actually run in a terminal. BASE selects the distro (and so the OpenSSH version).
ARG BASE=debian:trixie-slim
FROM ${BASE}
ENV DEBIAN_FRONTEND=noninteractive LANG=C.UTF-8
RUN apt-get update && apt-get install -y --no-install-recommends \
      openssh-server sudo tar gzip procps iproute2 vim htop tmux zsh fish less \
      libpam-google-authenticator ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /run/sshd && rm -f /etc/ssh/ssh_host_* \
    && echo 'LANG=C.UTF-8' > /etc/default/locale \
    && sed -i 's/^\(session\s*required\s*pam_loginuid.so\)/#\1/' /etc/pam.d/sshd \
    && sed -i 's/^@include common-auth/@include common-auth\nauth required pam_google_authenticator.so nullok/' /etc/pam.d/sshd \
    && useradd -m -s /bin/bash -G sudo tester && echo 'tester:test-password' | chpasswd \
    && useradd -m -s /bin/bash otpuser && echo 'otpuser:test-password' | chpasswd \
    && touch /home/tester/.zshrc /home/otpuser/.zshrc && chown tester /home/tester/.zshrc \
    && printf 'JBSWY3DPEHPK3PXP\n" RATE_LIMIT 3 30\n" WINDOW_SIZE 17\n" DISALLOW_REUSE\n" TOTP_AUTH\n11112222\n33334444\n55556666\n' > /home/otpuser/.google_authenticator \
    && chown otpuser /home/otpuser/.google_authenticator && chmod 400 /home/otpuser/.google_authenticator
COPY sshd_config /etc/ssh/sshd_config
COPY entrypoint.sh /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
