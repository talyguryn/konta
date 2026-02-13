# Konta Example: vps0

Complete working example of a Konta infrastructure repository.

**What's included:**
- ✅ Traefik (reverse proxy + SSL/TLS)
- ✅ Nginx (web server)
- ✅ Whoami (test service)
- ✅ Pre/success/failure deployment hooks
- ✅ Complete konta.yaml configuration

All accessible via `example.com` domain with automatic HTTPS.

## Quick Start

### 1. Customize for Your Domain

Replace `example.com` with your actual domain in all files:

```bash
# In nginx compose labels
traefik.http.routers.nginx.rule: "Host(`yourdomain.com`)"

# In Traefik ACME email
TRAEFIK_ACME_EMAIL=admin@yourdomain.com

# Update konta.yaml
url: https://github.com/yourname/infrastructure
```

### 2. Create Local Repository

```bash
# Clone or copy this to your machine
git clone https://github.com/yourname/infrastructure
cd infrastructure

# (or simply copy vps0/ folder)
```

### 3. Set Up DNS

Point your domain to your server:

```bash
# In your DNS provider (Cloudflare, Route53, etc):
A record: yourdomain.com        -> YOUR_SERVER_IP
A record: *.yourdomain.com      -> YOUR_SERVER_IP  (wildcard for whoami.yourdomain.com)
```

### 4. Install Konta

```bash
# On your server:
ssh root@your-server

# Download and install
wget https://github.com/kontacd/konta/releases/download/v0.1.3/konta-linux
chmod +x konta-linux
sudo mv konta-linux /usr/local/bin/konta

# Install with configuration
sudo konta install /path/to/infrastructure/vps0

# Follow prompts:
# URL: https://github.com/yourname/infrastructure
# Branch: main
# Path: vps0/apps
# Interval: 120
# Token: (if private repo)
# Enable daemon: yes
```

### 5. Deploy!

```bash
# On your workstation
git add .
git commit -m "Initial deployment"
git push

# On server: Automatically deployed within 2 minutes!
# Check logs:
ssh root@your-server "journalctl -u konta -f"
```

## File Structure

```
vps0/
├── konta.yaml                     # Konta configuration
│
├── apps/                          # Applications
│   ├── traefik/
│   │   ├── docker-compose.yml    # Reverse proxy config
│   │   └── acme.json             # (auto-generated SSL certs)
│   │
│   ├── nginx/
│   │   ├── docker-compose.yml
│   │   ├── nginx.conf
│   │   └── html/                  # Website content
│   │
│   └── whoami/
│       └── docker-compose.yml     # Test service
│
└── hooks/                         # Deployment hooks
    ├── pre.sh                     # Pre-deployment checks
    ├── success.sh                 # Post-success notification
    └── failure.sh                 # Error handling
```

## Customization

### Add Another Service

```bash
# Create directory
mkdir -p vps0/apps/myapp

# Create docker-compose.yml
cat > vps0/apps/myapp/docker-compose.yml << 'EOF'
version: '3.8'
services:
  app:
    image: myapp:latest
    labels:
      traefik.enable: "true"
      traefik.http.routers.myapp.rule: "Host(`myapp.yourdomain.com`)"
      traefik.http.routers.myapp.entrypoints: "web,websecure"
      traefik.http.services.myapp.loadbalancer.server.port: "3000"
    networks:
      - web

networks:
  web:
    external: true
    name: traefik_traefik
EOF

# Commit and push
git add vps0/apps/myapp/
git commit -m "Add myapp service"
git push

# Automatic deployment!
```

### Update Nginx Content

```bash
# Edit HTML files
nano vps0/apps/nginx/html/index.html

# Commit and push
git add vps0/apps/nginx/html/
git commit -m "Update website content"
git push

# Automatic deployment!
```

### Change SSL/TLS Settings

Edit in `vps0/apps/traefik/docker-compose.yml`:

```yaml
# For Let's Encrypt staging (free, unlimited):
command:
  - "--certificatesresolvers.letsencrypt.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory"

# For production Let's Encrypt (real certificates):
# (remove --acme.caserver, use default)
```

### Add Pre-deployment Checks

Edit `vps0/hooks/pre.sh`:

```bash
# Add your custom checks
# Example: Check database connectivity
psql -h db.example.com -U admin -c "SELECT 1" > /dev/null
if [ $? -ne 0 ]; then
    echo "Database is unreachable!"
    exit 1
fi
```

### Add Deployment Notifications

Edit `vps0/hooks/success.sh`:

```bash
# Uncomment and fill in your Slack webhook:
SLACK_WEBHOOK="https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXX"

curl -X POST "$SLACK_WEBHOOK" \
  -H 'Content-Type: application/json' \
  -d '{"text":"✅ Deployed!"}'
```

## Troubleshooting

### "Connection refused" on domains

Check DNS resolution:
```bash
nslookup yourdomain.com
# Should show YOUR_SERVER_IP
```

Check Traefik is running:
```bash
docker ps | grep traefik
# Should show traefik container running
```

### SSL certificate not issued

Check Traefik logs:
```bash
docker logs traefik
# Look for "certificate issued" or "ACME" errors
```

Traefik needs 80 (HTTP) and 443 (HTTPS) accessible:
```bash
curl http://yourdomain.com -L
# Should redirect to https:// and show content
```

### Service not accessible

Check service is running:
```bash
docker ps | grep nginx
# Should show nginx container

docker logs <container_name>
# Check for errors
```

Check network connectivity:
```bash
docker network ls | grep traefik
# Should show traefik network exists

docker network inspect traefik_traefik
# Should show all services connected
```

### Port conflicts

If Traefik fails to start (ports 80/443 in use):

```bash
# Find what's listening
lsof -i :80
lsof -i :443

# Kill the conflicting service or change Traefik ports in docker-compose.yml
ports:
  - "8080:80"    # Use 8080 externally
  - "8443:443"
```

## Next Steps

1. **Copy this example** to your infrastructure repo
2. **Customize** for your domain and services
3. **Test locally** with `docker compose config`
4. **Push to GitHub** and watch Konta deploy!
5. **Add more services** as needed (see "Add Another Service" above)

## Need Help?

- Check [USER_GUIDE.md](../docs/USER_GUIDE.md) for advanced configuration
- Review [HOW_IT_WORKS.md](../docs/HOW_IT_WORKS.md) to understand architecture
- See [README.md](../README.md) for general overview

---

**Happy deploying!** 🚀
