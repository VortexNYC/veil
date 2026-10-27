# CompleteRecoveryRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**proof_hmac** | **str** | hex(HMAC-SHA256(org master, request id)) — the sole-owner fast path that bypasses the 24h timelock. | [optional] 

## Example

```python
from veil.models.complete_recovery_request import CompleteRecoveryRequest

# TODO update the JSON string below
json = "{}"
# create an instance of CompleteRecoveryRequest from a JSON string
complete_recovery_request_instance = CompleteRecoveryRequest.from_json(json)
# print the JSON string representation of the object
print(CompleteRecoveryRequest.to_json())

# convert the object into a dict
complete_recovery_request_dict = complete_recovery_request_instance.to_dict()
# create an instance of CompleteRecoveryRequest from a dict
complete_recovery_request_from_dict = CompleteRecoveryRequest.from_dict(complete_recovery_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


