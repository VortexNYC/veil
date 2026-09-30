# VaultReport


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**items** | **int** |  | 
**weak** | **int** |  | 
**reused** | **int** |  | 
**pwned** | **int** |  | 
**hibp** | **str** |  | 
**findings** | [**List[VaultReportFinding]**](VaultReportFinding.md) |  | 

## Example

```python
from veil.models.vault_report import VaultReport

# TODO update the JSON string below
json = "{}"
# create an instance of VaultReport from a JSON string
vault_report_instance = VaultReport.from_json(json)
# print the JSON string representation of the object
print(VaultReport.to_json())

# convert the object into a dict
vault_report_dict = vault_report_instance.to_dict()
# create an instance of VaultReport from a dict
vault_report_from_dict = VaultReport.from_dict(vault_report_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


