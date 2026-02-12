#!/bin/bash
# Error handling hook for vps0
# Runs when deployment fails

echo ""
echo "=== Deployment Failed ❌ ==="
echo "Time: $(date)"
echo ""

echo "Check deployment logs:"
echo "  journalctl -u konta -n 50"
echo ""

# Example: Send urgent Slack alert
# Uncomment and update with your webhook URL:
# curl -X POST https://hooks.slack.com/services/YOUR/WEBHOOK/URL \
#   -H 'Content-Type: application/json' \
#   -d '{
#     "text": "❌ Deployment FAILED on vps0!",
#     "blocks": [
#       {
#         "type": "section",
#         "text": {
#           "type": "mrkdwn",
#           "text": "*⚠️ DEPLOYMENT FAILED*\n*Server:* vps0 (example.com)\n*Time:* '$(date)'\n\n_Check logs immediately_"
#         }
#       }
#     ]
#   }'

# Example: Page on-call engineer
# notify_pagerduty -d "Deployment failed on vps0" -u "https://your-logs.com"

echo ""
echo "Troubleshooting steps:"
echo "1. Check that docker-compose.yml files are valid"
echo "2. Verify Docker is running: docker ps"
echo "3. Check disk space: df -h /var/lib/docker"
echo "4. Review full logs: journalctl -u konta -p err"
