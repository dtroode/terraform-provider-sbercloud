package sbercloud

import (
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/chnsz/golangsdk"
	"github.com/chnsz/golangsdk/openstack/networking/v1/security/securitygroups"
	"github.com/chnsz/golangsdk/openstack/networking/v2/extensions/security/groups"
	"github.com/chnsz/golangsdk/openstack/networking/v2/extensions/security/rules"
	"github.com/huaweicloud/terraform-provider-huaweicloud/huaweicloud/config"
	"github.com/huaweicloud/terraform-provider-huaweicloud/huaweicloud/utils/fmtp"
	"github.com/huaweicloud/terraform-provider-huaweicloud/huaweicloud/utils/logp"
)

var sgRuleComputedSchema = &schema.Schema{
	Type:     schema.TypeList,
	Computed: true,
	Elem: &schema.Resource{
		Schema: map[string]*schema.Schema{
			"id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"direction": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"ethertype": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"port_range_min": {
				Type:     schema.TypeInt,
				Computed: true,
			},
			"port_range_max": {
				Type:     schema.TypeInt,
				Computed: true,
			},
			"protocol": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"remote_ip_prefix": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"remote_group_id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"description": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	},
}

func ResourceNetworkingSecGroupV2() *schema.Resource {
	return &schema.Resource{
		Create: resourceNetworkingSecGroupV2Create,
		Read:   resourceNetworkingSecGroupV2Read,
		Update: resourceNetworkingSecGroupV2Update,
		Delete: resourceNetworkingSecGroupV2Delete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Delete: schema.DefaultTimeout(10 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"region": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"name": {
				Type:     schema.TypeString,
				Required: true,
			},
			"description": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"enterprise_project_id": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},
			"delete_default_rules": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
			},
			"rules": sgRuleComputedSchema,

			"tenant_id": {
				Type:       schema.TypeString,
				Optional:   true,
				ForceNew:   true,
				Computed:   true,
				Deprecated: "tenant_id is deprecated",
			},
		},
	}
}

func resourceNetworkingSecGroupV2Create(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*config.Config)
	segClient, err := config.SecurityGroupV1Client(GetRegion(d, config))
	if err != nil {
		return fmtp.Errorf("Error creating SberCloud security group client: %s", err)
	}
	networkingClient, err := config.NetworkingV2Client(GetRegion(d, config))
	if err != nil {
		return fmtp.Errorf("Error creating SberCloud networking client: %s", err)
	}

	// only name and enterprise_project_id are supported
	opts := securitygroups.CreateOpts{
		Name:                d.Get("name").(string),
		EnterpriseProjectId: GetEnterpriseProjectID(d, config),
	}

	logp.Printf("[DEBUG] Create SberCloud Security Group: %#v", opts)
	securityGroup, err := securitygroups.Create(segClient, opts).Extract()
	if err != nil {
		return fmtp.Errorf("Error creating Security Group: %s", err)
	}

	d.SetId(securityGroup.ID)

	description := d.Get("description").(string)
	if description != "" {
		updateOpts := groups.UpdateOpts{
			Description: &description,
		}
		_, err = groups.Update(networkingClient, d.Id(), updateOpts).Extract()
		if err != nil {
			return fmtp.Errorf("Error updating description of security group %s: %s", d.Id(), err)
		}
	}

	// Delete the default security group rules if it has been requested.
	deleteDefaultRules := d.Get("delete_default_rules").(bool)
	if deleteDefaultRules {
		for _, rule := range securityGroup.SecurityGroupRules {
			if err := rules.Delete(networkingClient, rule.ID).ExtractErr(); err != nil {
				return fmtp.Errorf(
					"There was a problem deleting a default security group rule: %s", err)
			}
		}
	}

	return resourceNetworkingSecGroupV2Read(d, meta)
}

func resourceNetworkingSecGroupV2Read(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*config.Config)
	segClient, err := config.SecurityGroupV1Client(GetRegion(d, config))
	if err != nil {
		return fmtp.Errorf("Error creating SberCloud networking client: %s", err)
	}

	logp.Printf("[DEBUG] Retrieve information about security group: %s", d.Id())
	secGroup, err := securitygroups.Get(segClient, d.Id()).Extract()
	if err != nil {
		return CheckDeleted(d, err, "SberCloud Security group")
	}

	logp.Printf("[DEBUG] Retrieved Security Group %s: %+v", d.Id(), secGroup)

	mErr := multierror.Append(nil,
		d.Set("region", GetRegion(d, config)),
		d.Set("name", secGroup.Name),
		d.Set("description", secGroup.Description),
		d.Set("enterprise_project_id", secGroup.EnterpriseProjectId),
		d.Set("rules", flattenSecurityGroupRules(secGroup)),
	)
	if mErr.ErrorOrNil() != nil {
		return mErr
	}

	return nil
}

func flattenSecurityGroupRules(secGroup *securitygroups.SecurityGroup) []map[string]interface{} {
	sgRules := make([]map[string]interface{}, len(secGroup.SecurityGroupRules))
	for i, rule := range secGroup.SecurityGroupRules {
		sgRules[i] = map[string]interface{}{
			"id":               rule.ID,
			"direction":        rule.Direction,
			"protocol":         rule.Protocol,
			"ethertype":        rule.Ethertype,
			"port_range_max":   rule.PortRangeMax,
			"port_range_min":   rule.PortRangeMin,
			"remote_ip_prefix": rule.RemoteIpPrefix,
			"remote_group_id":  rule.RemoteGroupId,
			"description":      rule.Description,
		}
	}

	return sgRules
}

func resourceNetworkingSecGroupV2Update(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*config.Config)
	networkingClient, err := config.NetworkingV2Client(GetRegion(d, config))
	if err != nil {
		return fmtp.Errorf("Error creating SberCloud networking client: %s", err)
	}

	if d.HasChanges("name", "description") {
		description := d.Get("description").(string)
		updateOpts := groups.UpdateOpts{
			Name:        d.Get("name").(string),
			Description: &description,
		}

		logp.Printf("[DEBUG] Updating SecGroup %s with options: %#v", d.Id(), updateOpts)
		_, err = groups.Update(networkingClient, d.Id(), updateOpts).Extract()
		if err != nil {
			return fmtp.Errorf("Error updating SberCloud SecGroup: %s", err)
		}
	}

	return resourceNetworkingSecGroupV2Read(d, meta)
}

func resourceNetworkingSecGroupV2Delete(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*config.Config)
	segClient, err := config.SecurityGroupV1Client(GetRegion(d, config))
	if err != nil {
		return fmtp.Errorf("Error creating SberCloud networking client: %s", err)
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"ACTIVE"},
		Target:     []string{"DELETED"},
		Refresh:    waitForSecGroupDelete(segClient, d.Id()),
		Timeout:    d.Timeout(schema.TimeoutDelete),
		Delay:      5 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForState()
	if err != nil {
		return fmtp.Errorf("Error deleting SberCloud Security Group: %s", err)
	}

	d.SetId("")
	return nil
}

func waitForSecGroupDelete(segClient *golangsdk.ServiceClient, secGroupId string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		logp.Printf("[DEBUG] Attempting to delete SberCloud Security Group %s.\n", secGroupId)

		r, err := securitygroups.Get(segClient, secGroupId).Extract()
		if err != nil {
			if _, ok := err.(golangsdk.ErrDefault404); ok {
				logp.Printf("[DEBUG] Successfully deleted SberCloud Security Group %s", secGroupId)
				return r, "DELETED", nil
			}
			return r, "ACTIVE", err
		}

		err = securitygroups.Delete(segClient, secGroupId).ExtractErr()
		if err != nil {
			if _, ok := err.(golangsdk.ErrDefault404); ok {
				logp.Printf("[DEBUG] Successfully deleted SberCloud Security Group %s", secGroupId)
				return r, "DELETED", nil
			}
			if errCode, ok := err.(golangsdk.ErrUnexpectedResponseCode); ok {
				if errCode.Actual == 409 {
					return r, "ACTIVE", nil
				}
			}
			return r, "ACTIVE", err
		}

		logp.Printf("[DEBUG] SberCloud Security Group %s still active", secGroupId)
		return r, "ACTIVE", nil
	}
}