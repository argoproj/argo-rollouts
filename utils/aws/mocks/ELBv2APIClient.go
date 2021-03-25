// Code generated by mockery v0.0.0-dev. DO NOT EDIT.

package mocks

import (
	context "context"

	elasticloadbalancingv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	mock "github.com/stretchr/testify/mock"
)

// ELBv2APIClient is an autogenerated mock type for the ELBv2APIClient type
type ELBv2APIClient struct {
	mock.Mock
}

// DescribeListeners provides a mock function with given fields: _a0, _a1, _a2
func (_m *ELBv2APIClient) DescribeListeners(_a0 context.Context, _a1 *elasticloadbalancingv2.DescribeListenersInput, _a2 ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeListenersOutput, error) {
	_va := make([]interface{}, len(_a2))
	for _i := range _a2 {
		_va[_i] = _a2[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, _a0, _a1)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *elasticloadbalancingv2.DescribeListenersOutput
	if rf, ok := ret.Get(0).(func(context.Context, *elasticloadbalancingv2.DescribeListenersInput, ...func(*elasticloadbalancingv2.Options)) *elasticloadbalancingv2.DescribeListenersOutput); ok {
		r0 = rf(_a0, _a1, _a2...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*elasticloadbalancingv2.DescribeListenersOutput)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *elasticloadbalancingv2.DescribeListenersInput, ...func(*elasticloadbalancingv2.Options)) error); ok {
		r1 = rf(_a0, _a1, _a2...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DescribeLoadBalancers provides a mock function with given fields: _a0, _a1, _a2
func (_m *ELBv2APIClient) DescribeLoadBalancers(_a0 context.Context, _a1 *elasticloadbalancingv2.DescribeLoadBalancersInput, _a2 ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	_va := make([]interface{}, len(_a2))
	for _i := range _a2 {
		_va[_i] = _a2[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, _a0, _a1)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *elasticloadbalancingv2.DescribeLoadBalancersOutput
	if rf, ok := ret.Get(0).(func(context.Context, *elasticloadbalancingv2.DescribeLoadBalancersInput, ...func(*elasticloadbalancingv2.Options)) *elasticloadbalancingv2.DescribeLoadBalancersOutput); ok {
		r0 = rf(_a0, _a1, _a2...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*elasticloadbalancingv2.DescribeLoadBalancersOutput)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *elasticloadbalancingv2.DescribeLoadBalancersInput, ...func(*elasticloadbalancingv2.Options)) error); ok {
		r1 = rf(_a0, _a1, _a2...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DescribeRules provides a mock function with given fields: ctx, params, optFns
func (_m *ELBv2APIClient) DescribeRules(ctx context.Context, params *elasticloadbalancingv2.DescribeRulesInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeRulesOutput, error) {
	_va := make([]interface{}, len(optFns))
	for _i := range optFns {
		_va[_i] = optFns[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, params)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *elasticloadbalancingv2.DescribeRulesOutput
	if rf, ok := ret.Get(0).(func(context.Context, *elasticloadbalancingv2.DescribeRulesInput, ...func(*elasticloadbalancingv2.Options)) *elasticloadbalancingv2.DescribeRulesOutput); ok {
		r0 = rf(ctx, params, optFns...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*elasticloadbalancingv2.DescribeRulesOutput)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *elasticloadbalancingv2.DescribeRulesInput, ...func(*elasticloadbalancingv2.Options)) error); ok {
		r1 = rf(ctx, params, optFns...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DescribeTags provides a mock function with given fields: ctx, params, optFns
func (_m *ELBv2APIClient) DescribeTags(ctx context.Context, params *elasticloadbalancingv2.DescribeTagsInput, optFns ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTagsOutput, error) {
	_va := make([]interface{}, len(optFns))
	for _i := range optFns {
		_va[_i] = optFns[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, params)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *elasticloadbalancingv2.DescribeTagsOutput
	if rf, ok := ret.Get(0).(func(context.Context, *elasticloadbalancingv2.DescribeTagsInput, ...func(*elasticloadbalancingv2.Options)) *elasticloadbalancingv2.DescribeTagsOutput); ok {
		r0 = rf(ctx, params, optFns...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*elasticloadbalancingv2.DescribeTagsOutput)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *elasticloadbalancingv2.DescribeTagsInput, ...func(*elasticloadbalancingv2.Options)) error); ok {
		r1 = rf(ctx, params, optFns...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DescribeTargetGroups provides a mock function with given fields: _a0, _a1, _a2
func (_m *ELBv2APIClient) DescribeTargetGroups(_a0 context.Context, _a1 *elasticloadbalancingv2.DescribeTargetGroupsInput, _a2 ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetGroupsOutput, error) {
	_va := make([]interface{}, len(_a2))
	for _i := range _a2 {
		_va[_i] = _a2[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, _a0, _a1)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *elasticloadbalancingv2.DescribeTargetGroupsOutput
	if rf, ok := ret.Get(0).(func(context.Context, *elasticloadbalancingv2.DescribeTargetGroupsInput, ...func(*elasticloadbalancingv2.Options)) *elasticloadbalancingv2.DescribeTargetGroupsOutput); ok {
		r0 = rf(_a0, _a1, _a2...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*elasticloadbalancingv2.DescribeTargetGroupsOutput)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *elasticloadbalancingv2.DescribeTargetGroupsInput, ...func(*elasticloadbalancingv2.Options)) error); ok {
		r1 = rf(_a0, _a1, _a2...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
