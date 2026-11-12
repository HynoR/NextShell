#!/bin/sh
# Host keys are generated per container, so recreating a container changes the host key.
set -e
ssh-keygen -A >/dev/null
install -d -m 700 -o tester -g tester /home/tester/.ssh
printf '%s\n' "$AUTHORIZED_KEYS" > /home/tester/.ssh/authorized_keys
chown tester:tester /home/tester/.ssh/authorized_keys
chmod 600 /home/tester/.ssh/authorized_keys
exec /usr/sbin/sshd -D -e
