# VaultReportFinding


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**item_id** | **str** |  | 
**name** | **str** |  | 
**kind** | **str** |  | 
**uri** | **str** |  | [optional] 
**weak** | **List[str]** |  | [optional] 
**reused** | **int** |  | [optional] 
**pwned** | **int** |  | 

## Example

```python
from veil.models.vault_report_finding import VaultReportFinding

# TODO update the JSON string below
json = "{}"
# create an instance of VaultReportFinding from a JSON string
vault_report_finding_instance = VaultReportFinding.from_json(json)
# print the JSON string representation of the object
print(VaultReportFinding.to_json())

# convert the object into a dict
vault_report_finding_dict = vault_report_finding_instance.to_dict()
# create an instance of VaultReportFinding from a dict
vault_report_finding_from_dict = VaultReportFinding.from_dict(vault_report_finding_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


