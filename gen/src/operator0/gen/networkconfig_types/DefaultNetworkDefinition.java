public interface DefaultNetworkDefinition {
	//json:openshiftSDNConfig
	public OpenShiftSDNConfig getOpenShiftSDNConfig();
	//json:ovnKubernetesConfig
	public OVNKubernetesConfig getOVNKubernetesConfig();
	//json:otherConfig
	public Map<String,String> getOtherConfig();
}
}
