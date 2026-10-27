# ProvisionRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**recovery_pubkey** | **str** | Optional 32-byte X25519 public key (base64 or hex) to enroll the member&#39;s recovery share at signup. | [optional] 

## Example

```python
from veil.models.provision_request import ProvisionRequest

# TODO update the JSON string below
json = "{}"
# create an instance of ProvisionRequest from a JSON string
provision_request_instance = ProvisionRequest.from_json(json)
# print the JSON string representation of the object
print(ProvisionRequest.to_json())

# convert the object into a dict
provision_request_dict = provision_request_instance.to_dict()
# create an instance of ProvisionRequest from a dict
provision_request_from_dict = ProvisionRequest.from_dict(provision_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


