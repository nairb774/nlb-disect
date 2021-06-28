# nlb-disect

This is some junky code for inspecting the internal operation of the ALB NLB
deregistration delay and connection disconnection behaviors. The goal is to try
to find a reliable way to know when an NLB will stop sending new connections to
a target, as well as detect when an NLB will actually forcibly close the
connections. The AWS documentation around what happens when is incredibly vague,
which makes actually correctly terminating requests and connections gracefully
really challenging.
