# VerifyIssuerResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**code** | **str** |  | 
**issuer** | **str** |  | 
**message** | **str** |  | 

## Example

```python
from veil.models.verify_issuer_response import VerifyIssuerResponse

# TODO update the JSON string below
json = "{}"
# create an instance of VerifyIssuerResponse from a JSON string
verify_issuer_response_instance = VerifyIssuerResponse.from_json(json)
# print the JSON string representation of the object
print(VerifyIssuerResponse.to_json())

# convert the object into a dict
verify_issuer_response_dict = verify_issuer_response_instance.to_dict()
# create an instance of VerifyIssuerResponse from a dict
verify_issuer_response_from_dict = VerifyIssuerResponse.from_dict(verify_issuer_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


