# Detail: admission webhooks

**An admission webhook is the one case in which the API server calls operator code: before storing a write, it asks the webhook whether to allow or change it.** This repository has none; the control loop of [Operator and API server](readme.md) needs none.

An operator adds one for checks the CRD's rules cannot express, eg rejecting an unsupported combination of settings.

| step | who | what |
|---|---|---|
| 1. register | the installer | which writes to send, the HTTPS address of the webhook, and the certificate authority to verify it |
| 2. call | the API server, before storing a matching write | sends the submitted object to the webhook as an `AdmissionReview` |
| 3. answer | the webhook | allow, reject, or propose changes to the object |
| 4. store | the API server | on reject, the client gets an error; otherwise the object is stored and reaches the operator through its normal watch |

A webhook adds an incoming HTTPS endpoint for admission only. It does not replace the watch: the control loop still finds out about stored changes by "LIST" and "WATCH".

Reference: [admission webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/).
