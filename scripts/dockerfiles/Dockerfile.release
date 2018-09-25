FROM amazonlinux:2
RUN yum update -y && \
    yum install -y iproute && \
    yum install -y iptables

WORKDIR /app

COPY aws-cni /app 
COPY misc/10-aws.conflist /app

COPY portmap /app

COPY aws-k8s-agent  /app
COPY scripts/aws-cni-support.sh /app
COPY scripts/install-aws.sh /app
ENTRYPOINT /app/install-aws.sh
