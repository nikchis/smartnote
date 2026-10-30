# SSH server for the integration tests: user "tester" with password
# "testpass" and the public key passed as AUTHORIZED_KEY; TCP forwarding on.
FROM alpine:3.22
RUN apk add --no-cache openssh \
 && adduser -D -s /bin/sh tester \
 && echo 'tester:testpass' | chpasswd \
 && sed -i -E 's/^#?AllowTcpForwarding .*/AllowTcpForwarding yes/; s/^#?PasswordAuthentication .*/PasswordAuthentication yes/' /etc/ssh/sshd_config \
 && grep -q '^AllowTcpForwarding yes' /etc/ssh/sshd_config \
 && grep -q '^PasswordAuthentication yes' /etc/ssh/sshd_config
ARG AUTHORIZED_KEY=""
RUN install -d -m 700 -o tester -g tester /home/tester/.ssh \
 && printf '%s\n' "$AUTHORIZED_KEY" > /home/tester/.ssh/authorized_keys \
 && chown tester:tester /home/tester/.ssh/authorized_keys \
 && chmod 600 /home/tester/.ssh/authorized_keys
EXPOSE 22
# Host keys are made at start, so every container has its own.
CMD ["sh", "-c", "ssh-keygen -A && exec /usr/sbin/sshd -D -e"]
