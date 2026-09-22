# ProvisionResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**subject** | **str** |  | 
**org_id** | **str** |  | 

## Example

```python
from veil.models.provision_response import ProvisionResponse

# TODO update the JSON string below
json = "{}"
# create an instance of ProvisionResponse from a JSON string
provision_response_instance = ProvisionResponse.from_json(json)
# print the JSON string representation of the object
print(ProvisionResponse.to_json())

# convert the object into a dict
provision_response_dict = provision_response_instance.to_dict()
# create an instance of ProvisionResponse from a dict
provision_response_from_dict = ProvisionResponse.from_dict(provision_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


