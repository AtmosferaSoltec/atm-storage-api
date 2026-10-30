# /etc/nginx/sites-available/storage.atmosferast.com (symlinked from sites-enabled)
server {
	listen 80;
	listen [::]:80;
	server_name storage.atmosferast.com;
	return 301 https://$host$request_uri;
}

server {
	listen 443 ssl http2;
	listen [::]:443 ssl http2;
	server_name storage.atmosferast.com;

	ssl_certificate     /etc/ssl/atmosferast/origin.pem;
	ssl_certificate_key /etc/ssl/atmosferast/origin.key;

	# MAX_UPLOAD_BYTES (25 MiB) plus the multipart overhead the API allows (64 KiB).
	client_max_body_size 26m;

	location / {
		proxy_pass http://127.0.0.1:3132;
		proxy_http_version 1.1;
		proxy_set_header Host $host;
		proxy_set_header X-Real-IP $remote_addr;
		proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;
		# The API caps a request at 90 s.
		proxy_read_timeout 95s;
		proxy_send_timeout 95s;
	}
}
