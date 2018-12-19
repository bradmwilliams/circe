public interface ProxyConfig {
	//json:iptablesSyncPeriod
	public String getIptablesSyncPeriod();
	//json:bindAddress
	public String getBindAddress();
	//json:proxyArguments
	public Map<String,String[]> getProxyArguments();
}
