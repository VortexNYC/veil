# VerifyIssuerRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**issuer** | **str** | OIDC issuer URL to dry-run | 

## Example

```python
from veil.models.verify_issuer_request import VerifyIssuerRequest

# TODO update the JSON string below
json = "{}"
# create an instance of VerifyIssuerRequest from a JSON string
verify_issuer_request_instance = VerifyIssuerRequest.from_json(json)
# print the JSON string representation of the object
print(VerifyIssuerRequest.to_json())

# convert the object into a dict
verify_issuer_request_dict = verify_issuer_request_instance.to_dict()
# create an instance of VerifyIssuerRequest from a dict
verify_issuer_request_from_dict = VerifyIssuerRequest.from_dict(verify_issuer_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


