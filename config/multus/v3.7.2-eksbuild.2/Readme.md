## Changelog

Updated Multus image manifest to support arm64 architecture

docker manifest inspect 602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/multus-cni:v3.7.2-eksbuild.2
```
eksbuild.2
{
   "schemaVersion": 2,
   "mediaType": "application/vnd.docker.distribution.manifest.list.v2+json",
   "manifests": [
      {
         "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
         "size": 1161,
         "digest": "sha256:207e228c5c871ef42ebf9029bd3fea8ce2558ec96a0a95f670f5e5cba891836d",
         "platform": {
            "architecture": "amd64",
            "os": "linux"
         }
      },
      {
         "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
         "size": 1163,
         "digest": "sha256:e43fc57c63a01c0a64197408650efff767a8a76460a9ef02d87ea4fb5568409e",
         "platform": {
            "architecture": "arm64",
            "os": "linux"
         }
      }
   ]
}
```
