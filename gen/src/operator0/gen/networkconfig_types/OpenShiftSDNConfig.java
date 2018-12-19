public interface OpenShiftSDNConfig {
	//json:vxlanPort
	public Long getVXLANPort();
	//json:mtu
	public Long getMTU();
	//json:useExternalOpenvswitch
	public Boolean getUseExternalOpenvswitch();
}
