#!/bin/sh
# Wait for backend to be ready and get its IP dynamically
# This script runs at container startup, not build time

echo "Waiting for backend to be ready..."

# Wait for backend to respond (max 60 seconds)
RETRIES=30
DELAY=2
BACKEND_READY=false
BACKEND_HOST="litestat-backend"

for i in $(seq 1 $RETRIES); do
    echo "Attempt $i/$RETRIES: Checking $BACKEND_HOST..."
    
    # Try to connect to backend by unique alias
    if curl -s --connect-timeout 3 http://${BACKEND_HOST}:8080/health > /dev/null 2>&1; then
        BACKEND_READY=true
        echo "Backend is ready!"
        break
    fi
    
    sleep $DELAY
done

if [ "$BACKEND_READY" = false ]; then
    echo "ERROR: Backend did not become ready in time. Starting anyway..."
fi

# Get backend IP from DNS (use getent to get actual IP)
BACKEND_IP=$(getent hosts "$BACKEND_HOST" | head -1 | awk '{print $1}')

if [ -z "$BACKEND_IP" ]; then
    echo "Warning: Could not resolve '$BACKEND_HOST' hostname"
    BACKEND_IP="$BACKEND_HOST"
fi

echo "Backend '$BACKEND_HOST' resolved to IP: $BACKEND_IP"

# Generate nginx config that uses DIRECT IP, NOT resolver variable
# Disabled buffering AND gzip for API to ensure stable streaming of large responses
cat > /etc/nginx/conf.d/default.conf << EOF
server {
    listen 80;
    server_name localhost;
    
    # Increase client body size for large POST requests
    client_max_body_size 50M;
    
    root /usr/share/nginx/html;
    index index.html;
    
    # Gzip compression for static files ONLY
    gzip on;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;
    
    # Frontend static files
    location / {
        try_files \$uri \$uri/ /index.html;
    }
    
    # Proxy API requests to backend - STREAMING MODE
    location /api/ {
        proxy_pass http://${BACKEND_IP}:8080/api/;
        proxy_http_version 1.1;
        
        # Turn off gzip for API responses to avoid buffering issues with streaming
        gzip off;
        
        # Timeouts for long-running analysis
        proxy_connect_timeout 600s;
        proxy_send_timeout 600s;
        proxy_read_timeout 600s;
        
        # DISABLE BUFFERING - Stream response directly to client
        proxy_buffering off;
        proxy_request_buffering off;
        
        # Disable disk buffering
        proxy_max_temp_file_size 0;
        
        # Headers
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host \$host;
        proxy_cache_bypass \$http_upgrade;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    }
    
    # Health endpoint proxy
    location /health {
        proxy_pass http://${BACKEND_IP}:8080/health;
        proxy_http_version 1.1;
    }
}
EOF

echo "Nginx config generated with direct backend IP: $BACKEND_IP"
echo "Starting nginx..."
exec nginx -g "daemon off;"
