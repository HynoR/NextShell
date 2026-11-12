#!/bin/sh
sockd -f /etc/auth.conf -D
exec sockd -f /etc/open.conf
