# go to https://developer.microsoft.com/en-us/graph-explorer

# login to external EntraId tenant

# grant consent to the following permissions

# find guid of user flow
GET https://graph.microsoft.com/beta/identity/authenticationEventsFlows

# send PATCH request to the following endpoint wit the body below
PATCH https://graph.microsoft.com/beta/identity/authenticationEventsFlows/<user flow guid>

# body
{
    "@odata.type": "#microsoft.graph.externalUsersSelfServiceSignUpEventsFlow",
    "onInteractiveAuthFlowStart": {
        "@odata.type": "#microsoft.graph.onInteractiveAuthFlowStartExternalUsersSelfServiceSignUp",
        "isSignUpAllowed": "false"
    }
}