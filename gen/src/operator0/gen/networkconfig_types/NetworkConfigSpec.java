public interface NetworkConfigSpec {
	//json:clusterNetworks
	public ClusterNetwork getClusterNetworks();
	//json:serviceNetwork
	public String getServiceNetwork();
	//json:defaultNetwork
	public DefaultNetworkDefinition getDefaultNetwork();
	//json:additionalNetworks
	public AdditionalNetworkDefinition getAdditionalNetworks();
	//json:deployKubeProxy
	public Boolean getDeployKubeProxy();
	//json:kubeProxyConfig
	public ProxyConfig getKubeProxyConfig();
}
