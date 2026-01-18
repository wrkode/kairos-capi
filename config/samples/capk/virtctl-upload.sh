virtctl image-upload dv kairos-rootdisk \
  -n default \
  --size=6Gi \
  --image-path=/var/www/html/kairos-ubuntu-25.10-standard-amd64-generic-v3.6.0-k0sv1.34.1+k0s.1.iso \
  --storage-class=local-path \
  --access-mode=ReadWriteOnce \
  --force-bind \
  --uploadproxy-url=https://cdiupload.lab.k8 --insecure