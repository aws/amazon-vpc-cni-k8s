FROM debian:latest

RUN apt-get update &&  \
    apt-get install -y ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# If anyone has a better idea for how to trim undesired certs or a better ca list to use, I'm all ears
RUN cp /etc/ca-certificates.conf /tmp/caconf && cat /tmp/caconf | \
  grep -v "mozilla/CNNIC_ROOT\.crt" > /etc/ca-certificates.conf && \
  update-ca-certificates --fresh
