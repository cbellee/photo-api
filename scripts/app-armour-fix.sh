# 1. Check Ubuntu version
lsb_release -a

# 2. Disable the restrictive unprivileged userns AppArmor profile
sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0

# 3. Make it permanent
echo 'kernel.apparmor_restrict_unprivileged_userns=0' | sudo tee /etc/sysctl.d/99-k8s-apparmor.conf
sudo sysctl --system

# 4. Restart containerd
sudo systemctl restart containerd

# 5. Force-delete the stuck pods
kubectl -n photo-api delete pods --all --force --grace-period=0