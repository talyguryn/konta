#!/bin/bash
# Post-deployment hook for vps0
# Runs after successful deployment

echo ""
echo "=== Deployment Successful! ✅ ==="
echo "Time: $(date)"
echo ""

# Example: Send Slack notification
# Uncomment and update with your webhook URL:
# curl -X POST https://hooks.slack.com/services/YOUR/WEBHOOK/URL \
#   -H 'Content-Type: application/json' \
#   -d '{
#     "text": "✅ Deployment successful on vps0",
#     "blocks": [
#       {
#         "type": "section",
#         "text": {
#           "type": "mrkdwn",
#           "text": "*Deployment Status*\n✅ All services deployed successfully\n*Server:* vps0 (example.com)\n*Time:* '$(date)'"
#         }
#       }
#     ]
#   }'

# Example: Update external status
# curl -X POST https://status.example.com/api/deployment \
#   -H "Authorization: Bearer $STATUS_TOKEN" \
#   -d '{"status":"success","timestamp":"'$(date -Iseconds)'"}'

show_running_services() {
    echo "Currently running services:"
    docker ps --filter "label=konta.managed=true" --format "table {{.Names}}\t{{.Status}}" 2>/dev/null || echo "  (none with konta.managed label)"
}

show_running_services

echo ""
echo "Deployment log available at: journalctl -u konta -n 20"
