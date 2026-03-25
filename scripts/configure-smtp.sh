#!/bin/bash
# Configure SMTP for Aether invitation emails
# Edit the values below, then run: bash scripts/configure-smtp.sh

NAMESPACE="aether-be"

# ---- EDIT THESE VALUES ----
SMTP_USER="john@scharber.com"
SMTP_PASSWORD="vkcs uajr geyy ansc"   # Gmail App Password from myaccount.google.com/apppasswords
SMTP_FROM="${SMTP_USER}"
SMTP_FROM_NAME="Aether Platform"
APP_BASE_URL="https://aether.tas.scharber.com"
# ---------------------------

set -e

echo "Configuring SMTP for namespace: ${NAMESPACE}"

# Set credentials in secret
kubectl patch secret aether-backend-secret -n "${NAMESPACE}" --type merge -p "{\"stringData\":{
  \"SMTP_USER\": \"${SMTP_USER}\",
  \"SMTP_PASSWORD\": \"${SMTP_PASSWORD}\"
}}"
echo "Secret updated."

# Set config in configmap
kubectl patch configmap aether-backend-config -n "${NAMESPACE}" --type merge -p "{\"data\":{
  \"SMTP_ENABLED\": \"true\",
  \"SMTP_HOST\": \"smtp.gmail.com\",
  \"SMTP_PORT\": \"587\",
  \"SMTP_FROM\": \"${SMTP_FROM}\",
  \"SMTP_FROM_NAME\": \"${SMTP_FROM_NAME}\",
  \"APP_BASE_URL\": \"${APP_BASE_URL}\"
}}"
echo "ConfigMap updated."

# Restart backend pods
kubectl delete pods -n "${NAMESPACE}" -l app=aether-backend
echo "Pods restarting. Waiting for readiness..."

sleep 10
kubectl wait --for=condition=ready pod -l app=aether-backend -n "${NAMESPACE}" --timeout=60s
echo "Done. SMTP is now configured."
