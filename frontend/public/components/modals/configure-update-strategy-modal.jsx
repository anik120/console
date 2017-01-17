import React from 'react';
import ReactTooltip from 'react-tooltip';

import { createModalLauncher, ModalTitle, ModalBody, ModalSubmitFooter } from '../factory/modal';
import { PromiseComponent, pluralize } from '../utils';

const numberOrPercent = function(value) {
  if (typeof value === 'undefined') {
    return null;
  }
  if (typeof value === 'string' && value.indexOf('%') > -1) {
    return value;
  }

  return _.toInteger(value);
};

class ConfigureUpdateStrategyModal extends PromiseComponent {
  constructor(props) {
    super(props);

    this._onTypeChange = this._onTypeChange.bind(this);
    this._submit = this._submit.bind(this);
    this._cancel = this.props.cancel.bind(this);

    this.state = {
      strategyType: _.get(this.props.deployment.spec, 'strategy.type'),
      rollingUpdate: {}
    };
  }

  componentDidMount() {
    super.componentDidMount();
    ReactTooltip.rebuild();
  }

  _onTypeChange(event) {
    this.setState({ strategyType: event.target.value });
  }

  _submit(event) {
    event.preventDefault();
    const type = this.state.strategyType;
    this.props.deployment.spec.strategy.type = type;

    if (type === 'RollingUpdate') {
      this.props.deployment.spec.strategy.rollingUpdate = {};
      this.props.deployment.spec.strategy.rollingUpdate.maxUnavailable = numberOrPercent(event.target.elements['input-max-unavailable'].value);
      this.props.deployment.spec.strategy.rollingUpdate.maxSurge = numberOrPercent(event.target.elements['input-max-surge'].value);
    } else {
      this.props.deployment.spec.strategy.rollingUpdate = null;
    }
    this.props.close();
  }

  render() {
    return <form onSubmit={this._submit} name="form">
      <ModalTitle>Deployment Update Strategy</ModalTitle>
      <ModalBody>
        <div className="co-m-form-row">
          <p>
            How should the pods be replaced when a new revision is created?
          </p>
        </div>

        <div className="row co-m-form-row">
          <div className="col-sm-12">

            <input value="RollingUpdate" type="radio"
              id="configure-update-strategy--rollingUpdate"
              autoFocus="true"
              checked={this.state.strategyType === 'RollingUpdate'}
              onChange={this._onTypeChange} />
            <label htmlFor="configure-update-strategy--rollingUpdate">
              RollingUpdate <span className="co-no-bold">(default)</span>
            </label>
            <div className="co-m-radio-desc">
              <p className="text-muted">
                Execute a smooth roll out of the new revision, based on the settings below
              </p>

            <div className="row co-m-form-row">
              <div className="col-sm-3">
                <label htmlFor="input-max-unavailable" className="control-label">
                  Max Unavailable:
                </label>
              </div>
              <div className="co-m-form-col col-sm-9">
                <div className="form-inline">
                  <div className="input-group">
                    <input disabled={this.state.strategyType !== 'RollingUpdate'}
                      placeholder="1" size="5" type="text" className="form-control"
                      id="input-max-unavailable" />
                    <span className="input-group-addon" data-tip="Current desired pod count">
                      of { pluralize(this.props.deployment.spec.replicas, 'pod')}
                    </span>
                  </div>
                </div>
                <p className="help-block text-muted">Number or percentage of total pods at the start of the update (optional)</p>
              </div>
            </div>

            <div className="row co-m-form-row">
              <div className="col-sm-3">
                <label htmlFor="input-max-surge" className="control-label">Max Surge:</label>
              </div>
              <div className="co-m-form-col col-sm-9">
                <div className="form-inline">
                  <div className="input-group">
                    <input  disabled={this.state.strategyType !== 'RollingUpdate'} placeholder="1" size="5" type="text" className="form-control"
                      id="input-max-surge" />
                    <span className="input-group-addon" data-tip="Current desired pod count">
                      greater than { pluralize(this.props.deployment.spec.replicas, 'pod')}
                    </span>
                  </div>
                </div>
                <p className="help-block text-muted">Number or percentage of total pods at the start of the update (optional)</p>
              </div>
            </div>

            <div className="co-m-form-row"></div>
          </div>
        </div>

        <div className="col-sm-12">
          <input
            checked={this.state.strategyType === 'Recreate'}
            onChange={this._onTypeChange}
            value="Recreate" type="radio" id="configure-update-strategy--recreate"/>
          <label htmlFor="configure-update-strategy--recreate">Recreate</label>
          <p className="co-m-radio-desc text-muted">
            Shut down all existing pods before creating new ones
          </p>
        </div>

      </div>

      </ModalBody>
      <ModalSubmitFooter
        errorFormatter="k8sApi"
        submitText="Save Strategy"
        cancel={this._cancel} />
    </form>;
  }
}

ConfigureUpdateStrategyModal.propTypes = {
  deployment: React.PropTypes.object
};

export const configureUpdateStrategyModal = createModalLauncher(ConfigureUpdateStrategyModal);
