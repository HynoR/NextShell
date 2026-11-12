#!/bin/sh
# Host keys are generated per container, so recreating a container changes the host key.
set -e
ssh-keygen -A >/dev/null
install -d -m 700 -o tester -g tester /home/tester/.ssh
printf '%s\n' "$AUTHORIZED_KEYS" > /home/tester/.ssh/authorized_keys
chown tester:tester /home/tester/.ssh/authorized_keys
chmod 600 /home/tester/.ssh/authorized_keys
# OpenSSH >= 9.8 drops a source address for a while after failed logins; the tests fail logins on purpose
# from one address. Prepend (a later Match block would reject it) and only if this sshd knows the keyword.
sed -i '1i PerSourcePenalties no' /etc/ssh/sshd_config
/usr/sbin/sshd -t 2>/dev/null || sed -i '1d' /etc/ssh/sshd_config
exec /usr/sbin/sshd -D -e
