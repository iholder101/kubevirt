package apply

import "kubevirt.io/client-go/log"

func (r *Reconciler) updateKubeVirtSystem(controllerDeploymentsRolledOver bool) (bool, error) {
	// UPDATE PATH IS
	// 1. daemonsets - ensures all compute nodes are updated to handle new features
	// 2. wait for daemonsets to roll over
	// 3. controllers - ensures control plane is ready for new features
	// 4. wait for controllers to roll over
	// 5. apiserver - toggles on new features.

	// create/update Daemonsets
	for _, daemonSet := range r.targetStrategy.DaemonSets() {
		finished, err := r.syncDaemonSet(daemonSet)
		if !finished || err != nil {
			log.Log.Infof("ihol3 updateKubeVirtSystem(): not finished syncDaemonSet")
			return false, err
		}
	}

	// create/update Controller Deployments
	for _, deployment := range r.targetStrategy.ControllerDeployments() {
		deployment, err := r.syncDeployment(deployment)
		if err != nil {
			log.Log.Infof("ihol3 updateKubeVirtSystem(): not finished syncDeployment")
			return false, err
		}
		err = r.syncPodDisruptionBudgetForDeployment(deployment)
		if err != nil {
			log.Log.Infof("ihol3 updateKubeVirtSystem(): not finished syncPodDisruptionBudgetForDeployment")
			return false, err
		}
	}

	// wait for controllers
	if !controllerDeploymentsRolledOver {
		log.Log.Infof("ihol3 updateKubeVirtSystem(): not finished !controllerDeploymentsRolledOver")
		// not rolled out yet
		return false, nil
	}

	// create/update ExportProxy Deployments
	for _, deployment := range r.targetStrategy.ExportProxyDeployments() {
		if r.exportProxyEnabled() {
			deployment, err := r.syncDeployment(deployment)
			if err != nil {
				log.Log.Infof("ihol3 updateKubeVirtSystem() - ExportProxyDeployments(): not finished syncDeployment")
				return false, err
			}
			err = r.syncPodDisruptionBudgetForDeployment(deployment)
			if err != nil {
				log.Log.Infof("ihol3 updateKubeVirtSystem() - ExportProxyDeployments(): not finished syncPodDisruptionBudgetForDeployment")
				return false, err
			}
		} else if err := r.deleteDeployment(deployment); err != nil {
			log.Log.Infof("ihol3 updateKubeVirtSystem() - ExportProxyDeployments(): not finished deleteDeployment")
			return false, err
		}
	}

	// create/update API Deployments
	for _, deployment := range r.targetStrategy.ApiDeployments() {
		deployment, err := r.syncDeployment(deployment)
		if err != nil {
			log.Log.Infof("ihol3 updateKubeVirtSystem() - ApiDeployments(): not finished syncDeployment")
			return false, err
		}
		err = r.syncPodDisruptionBudgetForDeployment(deployment)
		if err != nil {
			log.Log.Infof("ihol3 updateKubeVirtSystem() - ApiDeployments(): not finished syncPodDisruptionBudgetForDeployment")
			return false, err
		}
	}

	return true, nil
}
