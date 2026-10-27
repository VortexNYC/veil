# EnrollRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**pubkey** | **str** | Member&#39;s 32-byte X25519 public key, base64 or hex. | 

## Example

```python
from veil.models.enroll_request import EnrollRequest

# TODO update the JSON string below
json = "{}"
# create an instance of EnrollRequest from a JSON string
enroll_request_instance = EnrollRequest.from_json(json)
# print the JSON string representation of the object
print(EnrollRequest.to_json())

# convert the object into a dict
enroll_request_dict = enroll_request_instance.to_dict()
# create an instance of EnrollRequest from a dict
enroll_request_from_dict = EnrollRequest.from_dict(enroll_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


