# SSH server for the integration tests: user "tester" with password
# "testpass" and the public key passed at start as AUTHORIZED_KEY; TCP
# forwarding on. The base is pinned and nothing varies at build time, so the
# image tagged by the tests is reused from the cache instead of fetching
# packages again.
FROM alpine:3.22.6@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
RUN apk add --no-cache openssh \
 && adduser -D -s /bin/sh tester \
 && echo 'tester:testpass' | chpasswd \
 && sed -i -E 's/^#?AllowTcpForwarding .*/AllowTcpForwarding yes/; s/^#?PasswordAuthentication .*/PasswordAuthentication yes/' /etc/ssh/sshd_config \
 && grep -q '^AllowTcpForwarding yes' /etc/ssh/sshd_config \
 && grep -q '^PasswordAuthentication yes' /etc/ssh/sshd_config \
 && install -d -m 700 -o tester -g tester /home/tester/.ssh
EXPOSE 22
# Host keys are made at start, so every container has its own.
CMD ["sh", "-c", "printf '%s\\n' \"$AUTHORIZED_KEY\" > /home/tester/.ssh/authorized_keys && chown tester:tester /home/tester/.ssh/authorized_keys && chmod 600 /home/tester/.ssh/authorized_keys && ssh-keygen -A && exec /usr/sbin/sshd -D -e"]
