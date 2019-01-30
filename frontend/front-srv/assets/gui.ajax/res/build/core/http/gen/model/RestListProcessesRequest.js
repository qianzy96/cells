/**
 * Pydio Cells Rest API
 * No description provided (generated by Swagger Codegen https://github.com/swagger-api/swagger-codegen)
 *
 * OpenAPI spec version: 1.0
 * 
 *
 * NOTE: This class is auto generated by the swagger code generator program.
 * https://github.com/swagger-api/swagger-codegen.git
 * Do not edit the class manually.
 *
 */

'use strict';

exports.__esModule = true;

function _interopRequireDefault(obj) { return obj && obj.__esModule ? obj : { 'default': obj }; }

function _classCallCheck(instance, Constructor) { if (!(instance instanceof Constructor)) { throw new TypeError('Cannot call a class as a function'); } }

var _ApiClient = require('../ApiClient');

var _ApiClient2 = _interopRequireDefault(_ApiClient);

/**
* The RestListProcessesRequest model module.
* @module model/RestListProcessesRequest
* @version 1.0
*/

var RestListProcessesRequest = (function () {
    /**
    * Constructs a new <code>RestListProcessesRequest</code>.
    * @alias module:model/RestListProcessesRequest
    * @class
    */

    function RestListProcessesRequest() {
        _classCallCheck(this, RestListProcessesRequest);

        this.PeerId = undefined;
        this.ServiceName = undefined;
    }

    /**
    * Constructs a <code>RestListProcessesRequest</code> from a plain JavaScript object, optionally creating a new instance.
    * Copies all relevant properties from <code>data</code> to <code>obj</code> if supplied or a new instance if not.
    * @param {Object} data The plain JavaScript object bearing properties of interest.
    * @param {module:model/RestListProcessesRequest} obj Optional instance to populate.
    * @return {module:model/RestListProcessesRequest} The populated <code>RestListProcessesRequest</code> instance.
    */

    RestListProcessesRequest.constructFromObject = function constructFromObject(data, obj) {
        if (data) {
            obj = obj || new RestListProcessesRequest();

            if (data.hasOwnProperty('PeerId')) {
                obj['PeerId'] = _ApiClient2['default'].convertToType(data['PeerId'], 'String');
            }
            if (data.hasOwnProperty('ServiceName')) {
                obj['ServiceName'] = _ApiClient2['default'].convertToType(data['ServiceName'], 'String');
            }
        }
        return obj;
    };

    /**
    * @member {String} PeerId
    */
    return RestListProcessesRequest;
})();

exports['default'] = RestListProcessesRequest;
module.exports = exports['default'];

/**
* @member {String} ServiceName
*/