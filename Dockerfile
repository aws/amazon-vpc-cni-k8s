FROM golang:1.8
WORKDIR /app

# Set an env var that matches your github repo name

ENV SRC_DIR=/go/src/github.com/aws/amazon-vpc-cni-k8s/

# Add the source code:
ADD . $SRC_DIR

# Build it:
RUN cd $SRC_DIR; go build -o myapp; cp myapp /app/
ENTRYPOINT ["./myapp"]